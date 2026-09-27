// Package engine wires the pipeline together: it fans source lines to the
// detector and the context ring, curates and fingerprints detected events,
// tracks incidents, and drives the backend worker pool and the sink. It is the
// only place that holds all four interfaces at once.
package engine

import (
	"context"
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

// Options configures a single-engine run. Source, Backend, and Sink are the
// seams tests replace with fakes.
type Options struct {
	Source        source.Source
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

// Engine owns one run of the pipeline. It is single-use: build a new one per
// Run.
type Engine struct {
	source   source.Source
	detector detect.Detector
	curator  *cctx.Curator
	ring     *cctx.Ring
	backend  backend.Backend
	sink     sink.Sink
	tracker  *incident.Tracker
	log      *slog.Logger
	workers  int
	buffer   int
	dropped  atomic.Int64
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

	// The ring must outlast the longest possible block plus the window on
	// either side, or preceding context would be evicted before it is used.
	capacity := opts.MaxBlockLines + opts.ContextBefore + opts.ContextAfter + 128
	if capacity < defaultBuffer {
		capacity = defaultBuffer
	}
	ring := cctx.NewRing(capacity)

	return &Engine{
		source:   opts.Source,
		detector: detect.New(opts.ContextAfter, opts.MaxBlockLines),
		curator:  cctx.New(opts.ContextBefore, opts.ContextBudget, ring),
		ring:     ring,
		backend:  opts.Backend,
		sink:     opts.Sink,
		tracker:  incident.NewTracker(opts.ExplainWindow),
		log:      log,
		workers:  workers,
		buffer:   buffer,
	}
}

// Dropped reports how many results were discarded because the sink could not
// keep up.
func (e *Engine) Dropped() int64 { return e.dropped.Load() }

// Run drives the pipeline until the context is cancelled or the source ends.
func (e *Engine) Run(ctx context.Context) error {
	lines, err := e.source.Stream(ctx)
	if err != nil {
		return err
	}

	detectorIn := make(chan source.Line, e.buffer)
	events := make(chan detect.Event, e.buffer)
	sinkIn := make(chan sink.Result, e.buffer)

	var producers sync.WaitGroup
	producers.Add(2)
	go func() {
		defer producers.Done()
		e.feed(ctx, lines, detectorIn)
	}()
	go func() {
		defer producers.Done()
		defer close(events)
		if err := e.detector.Detect(ctx, detectorIn, events); err != nil {
			e.log.Error("detector stopped", "error", err)
		}
	}()

	var workers sync.WaitGroup
	for i := 0; i < e.workers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			e.work(ctx, events, sinkIn)
		}()
	}

	var sinkDone sync.WaitGroup
	sinkDone.Add(1)
	go func() {
		defer sinkDone.Done()
		e.drainSink(ctx, sinkIn)
	}()

	producers.Wait()
	workers.Wait()
	close(sinkIn)
	sinkDone.Wait()
	return nil
}

// feed keeps the ring current and forwards lines to the detector. Sending to
// the detector applies backpressure to the source, which is what bounds memory.
func (e *Engine) feed(ctx context.Context, in <-chan source.Line, out chan<- source.Line) {
	defer close(out)
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-in:
			if !ok {
				return
			}
			e.ring.Add(line)
			select {
			case out <- line:
			case <-ctx.Done():
				return
			}
		}
	}
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
	curation := e.curator.Build(event)
	fp := fingerprint.Of(event.Kind, curation.Block)
	sourceName := event.Trigger.Source

	inc, due := e.tracker.Observe(fp.Hash, event.Kind, sourceName, time.Now())
	if due {
		explanation, err := e.backend.Explain(ctx, backend.Request{
			Kind:        event.Kind,
			Source:      sourceName,
			Excerpt:     curation.Excerpt,
			Fingerprint: fp.Hash,
		})
		if err != nil {
			inc = e.tracker.RecordExplanation(fp.Hash, nil, e.backend.Name(), err.Error())
		} else {
			inc = e.tracker.RecordExplanation(fp.Hash, &explanation, e.backend.Name(), "")
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
