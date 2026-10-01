// Package engine wires the pipeline together: it fans each source's lines into
// a per-label stream, curates and fingerprints detected events, tracks
// incidents, and drives the backend worker pool and the sink. It is the only
// place that holds all four interfaces at once.
package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shaheeranser/watcher/internal/backend"
	cctx "github.com/shaheeranser/watcher/internal/context"
	"github.com/shaheeranser/watcher/internal/detect"
	"github.com/shaheeranser/watcher/internal/fingerprint"
	"github.com/shaheeranser/watcher/internal/incident"
	"github.com/shaheeranser/watcher/internal/sink"
	"github.com/shaheeranser/watcher/internal/source"
)

const defaultBuffer = 1024

// Options configures a single-engine run. Sources, Backend, and Sink are the
// seams tests replace with fakes.
type Options struct {
	Sources       []source.Source
	Backend       backend.Backend
	Sink          sink.Sink
	Logger        *slog.Logger
	ContextBefore int
	ContextAfter  int
	ContextBudget int
	MaxBlockLines int
	Workers       int
	ExplainWindow time.Duration
	Buffer        int
}

// stream is one labeled pipeline: a ring of recent lines, the curator that
// builds an excerpt from them, and the detector that assembles crash blocks. It
// is per label, so a panic on one source can never be assembled with lines from
// another.
type stream struct {
	ring     *cctx.Ring
	curator  *cctx.Curator
	detector detect.Detector
	in       chan source.Line
}

// Engine owns one run of the pipeline. It is single-use: build a new one per
// Run.
type Engine struct {
	sources []source.Source
	backend backend.Backend
	sink    sink.Sink
	tracker *incident.Tracker
	log     *slog.Logger
	workers int
	buffer  int
	dropped atomic.Int64

	contextBefore int
	contextAfter  int
	contextBudget int
	maxBlockLines int

	mu      sync.Mutex
	streams map[string]*stream
}

func New(opts Options) *Engine {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	buffer := opts.Buffer
	if buffer <= 0 {
		buffer = defaultBuffer
	}
	workers := opts.Workers
	if workers < 1 {
		workers = 1
	}
	return &Engine{
		sources:       opts.Sources,
		backend:       opts.Backend,
		sink:          opts.Sink,
		tracker:       incident.NewTracker(opts.ExplainWindow),
		log:           log,
		workers:       workers,
		buffer:        buffer,
		contextBefore: opts.ContextBefore,
		contextAfter:  opts.ContextAfter,
		contextBudget: opts.ContextBudget,
		maxBlockLines: opts.MaxBlockLines,
		streams:       make(map[string]*stream),
	}
}

// Dropped reports how many results were discarded because the sink could not
// keep up.
func (e *Engine) Dropped() int64 { return e.dropped.Load() }

// Run drives the pipeline until the context is cancelled or every source has
// ended. A source reaching EOF stops only that source; the run ends 0 when the
// last one is done, and cancellation stops it immediately (RT-SRC-6).
func (e *Engine) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	lines := make([]<-chan source.Line, len(e.sources))
	for i, src := range e.sources {
		ch, err := src.Stream(runCtx)
		if err != nil {
			return fmt.Errorf("source %q: %w", src.Name(), err)
		}
		lines[i] = ch
	}

	events := make(chan detect.Event, e.buffer)
	sinkIn := make(chan sink.Result, e.buffer)

	var pipelines sync.WaitGroup
	var routers sync.WaitGroup
	for _, ch := range lines {
		routers.Add(1)
		go func(in <-chan source.Line) {
			defer routers.Done()
			e.route(runCtx, in, events, &pipelines)
		}(ch)
	}

	// Every source ending closes only its own streams; the shared event channel
	// closes once all of them, and their detectors, have finished.
	go func() {
		routers.Wait()
		pipelines.Wait()
		close(events)
	}()

	var workers sync.WaitGroup
	for i := 0; i < e.workers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			e.work(runCtx, events, sinkIn)
		}()
	}

	var sinkDone sync.WaitGroup
	sinkDone.Add(1)
	go func() {
		defer sinkDone.Done()
		e.drainSink(runCtx, sinkIn)
	}()

	workers.Wait()
	close(sinkIn)
	sinkDone.Wait()
	return nil
}

// route fans one source's lines into per-label streams, keeping each label's
// ring current. A source that ends closes only the streams it created.
func (e *Engine) route(ctx context.Context, in <-chan source.Line, events chan<- detect.Event, pipelines *sync.WaitGroup) {
	var owned []*stream
	defer func() {
		for _, st := range owned {
			close(st.in)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-in:
			if !ok {
				return
			}
			st, created := e.streamFor(ctx, line.Source, events, pipelines)
			if created {
				owned = append(owned, st)
			}
			st.ring.Add(line)
			select {
			case st.in <- line:
			case <-ctx.Done():
				return
			}
		}
	}
}

// streamFor returns the pipeline for a label, creating and starting it on first
// sight. The ring outlasts the longest possible block plus its context on
// either side, or preceding lines would be evicted before the curator reads
// them.
func (e *Engine) streamFor(ctx context.Context, label string, events chan<- detect.Event, pipelines *sync.WaitGroup) (*stream, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if st, ok := e.streams[label]; ok {
		return st, false
	}

	capacity := e.maxBlockLines + e.contextBefore + e.contextAfter + 128
	if capacity < defaultBuffer {
		capacity = defaultBuffer
	}
	ring := cctx.NewRing(capacity)
	st := &stream{
		ring:     ring,
		curator:  cctx.New(e.contextBefore, e.contextBudget, ring),
		detector: detect.New(e.contextAfter, e.maxBlockLines),
		in:       make(chan source.Line, e.buffer),
	}
	e.streams[label] = st

	pipelines.Add(1)
	go func() {
		defer pipelines.Done()
		if err := st.detector.Detect(ctx, st.in, events); err != nil {
			e.log.Error("detector stopped", "label", label, "error", err)
		}
	}()
	return st, true
}

func (e *Engine) stream(label string) *stream {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.streams[label]
}

func (e *Engine) work(ctx context.Context, events <-chan detect.Event, out chan<- sink.Result) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			e.handle(ctx, event, out)
		}
	}
}

func (e *Engine) handle(ctx context.Context, event detect.Event, out chan<- sink.Result) {
	label := event.Trigger.Source
	st := e.stream(label)
	if st == nil {
		return
	}
	curation := st.curator.Build(event)
	fp := fingerprint.Of(event.Kind, curation.Block)

	inc, due := e.tracker.Observe(label, fp.Hash, event.Kind, time.Now())
	if due {
		explanation, err := e.backend.Explain(ctx, backend.Request{
			Kind:        event.Kind,
			Source:      label,
			Excerpt:     curation.Excerpt,
			Fingerprint: fp.Hash,
		})
		if err != nil {
			inc = e.tracker.RecordExplanation(label, fp.Hash, nil, e.backend.Name(), err.Error())
		} else {
			inc = e.tracker.RecordExplanation(label, fp.Hash, &explanation, e.backend.Name(), "")
		}
	}

	e.emit(ctx, out, sink.Result{
		Fingerprint: inc.Fingerprint,
		Kind:        inc.Kind,
		Source:      inc.Source,
		Count:       inc.Count,
		FirstSeen:   inc.FirstSeen,
		LastSeen:    inc.LastSeen,
		Explanation: inc.Explanation,
		ExplainErr:  inc.ExplainErr,
		Model:       inc.Model,
	})
}

// emit never blocks: a full sink queue drops the result and counts it, which is
// the documented slow-sink policy (CORE-SRC-4). Blocking instead would stall
// the worker and, through it, the whole pipeline.
func (e *Engine) emit(ctx context.Context, out chan<- sink.Result, r sink.Result) {
	select {
	case out <- r:
	case <-ctx.Done():
	default:
		if dropped := e.dropped.Add(1); dropped == 1 || dropped%100 == 0 {
			e.log.Warn("dropping result: sink is not keeping up", "dropped", dropped)
		}
	}
}

func (e *Engine) drainSink(ctx context.Context, in <-chan sink.Result) {
	for r := range in {
		if err := e.sink.Emit(ctx, r); err != nil {
			e.log.Error("sink failed", "sink", e.sink.Name(), "error", err)
		}
	}
}
