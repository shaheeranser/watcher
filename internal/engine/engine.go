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

// minResolveTick and maxResolveTick bound how often the resolve scan runs: often
// enough that a resolution is prompt, rarely enough that it costs nothing.
const (
	minResolveTick = time.Second
	maxResolveTick = 30 * time.Second
)

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

	// Notifications, when set, receives the incident state machine's
	// new/ongoing/resolved notifications. It is the push channel; Sink stays the
	// local result stream, which reports every occurrence. A nil value disables
	// push without stopping the state machine, so incident state still
	// transitions for a read-only consumer.
	Notifications sink.Sink

	// ThrottleWindow is the state machine's T (minimum interval between ongoing
	// notifications); ResolveWindow is W (quiet period before resolved).
	ThrottleWindow time.Duration
	ResolveWindow  time.Duration

	Buffer int

	// now is a test seam so the state machine's windows can be exercised
	// without real sleeps.
	now func() time.Time

	// resolveTick overrides the derived resolve-scan interval so a test can
	// drive resolution without waiting out a real window.
	resolveTick time.Duration
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

// Stats is a snapshot of the engine's liveness counters for the heartbeat.
type Stats struct {
	LinesProcessed    int64
	IncidentsTracked  int
	NotificationsSent int64
}

// Engine owns one run of the pipeline. It is single-use: build a new one per
// Run.
type Engine struct {
	sources       []source.Source
	backend       backend.Backend
	sink          sink.Sink
	notifications sink.Sink
	tracker       *incident.Tracker
	notifier      *incident.Notifier
	log           *slog.Logger
	workers       int
	buffer        int
	now           func() time.Time

	resolveWindow time.Duration
	resolveTick   time.Duration

	dropped           atomic.Int64
	lines             atomic.Int64
	notificationsSent atomic.Int64

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
	now := opts.now
	if now == nil {
		now = time.Now
	}
	return &Engine{
		sources:       opts.Sources,
		backend:       opts.Backend,
		sink:          opts.Sink,
		notifications: opts.Notifications,
		tracker:       incident.NewTracker(opts.ExplainWindow),
		notifier:      incident.NewNotifier(opts.ThrottleWindow, opts.ResolveWindow),
		log:           log,
		workers:       workers,
		buffer:        buffer,
		now:           now,
		resolveWindow: opts.ResolveWindow,
		resolveTick:   opts.resolveTick,
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

// Stats reports the liveness counters the heartbeat publishes.
func (e *Engine) Stats() Stats {
	return Stats{
		LinesProcessed:    e.lines.Load(),
		IncidentsTracked:  e.tracker.Len(),
		NotificationsSent: e.notificationsSent.Load(),
	}
}

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

	// The resolve scan runs on its own schedule, independent of incident
	// traffic, so an incident that simply stops is still announced
	// (PROD-STM-3).
	var resolveDone sync.WaitGroup
	if tick := e.resolveInterval(); tick > 0 {
		resolveDone.Add(1)
		go func() {
			defer resolveDone.Done()
			e.resolveLoop(runCtx, tick)
		}()
	}

	workers.Wait()
	close(sinkIn)
	sinkDone.Wait()
	cancel()
	resolveDone.Wait()
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
			e.lines.Add(1)
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
	now := e.now()

	obs := e.notifier.Observe(label, fp.Hash, now)
	if obs.Fresh {
		e.tracker.Reset(label, fp.Hash)
	}
	inc, due := e.tracker.Observe(label, fp.Hash, event.Kind, now)

	// The notification goes out before the model call, so alert latency is
	// independent of model latency; the explanation follows as a second
	// notification when it lands (PROD-STM-8).
	if obs.Notify {
		e.push(ctx, inc, obs.Kind)
	}

	if due {
		explanation, err := e.backend.Explain(ctx, backend.Request{
			Kind:        event.Kind,
			Source:      label,
			Excerpt:     curation.Excerpt,
			Fingerprint: fp.Hash,
		})
		if err != nil {
			inc = e.tracker.RecordExplanation(label, fp.Hash, nil, e.backend.Model(), err.Error())
		} else {
			inc = e.tracker.RecordExplanation(label, fp.Hash, &explanation, e.backend.Model(), "")
		}
		if kind, ok := e.notifier.Explained(label, fp.Hash, e.now()); ok {
			e.push(ctx, inc, kind)
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

// push delivers one lifecycle notification to the push channel. It is separate
// from the local result stream so suppressed occurrences never reach it.
func (e *Engine) push(ctx context.Context, inc incident.Incident, kind incident.NotificationKind) {
	if e.notifications == nil {
		return
	}
	r := sink.Result{
		Fingerprint:  inc.Fingerprint,
		Kind:         inc.Kind,
		Source:       inc.Source,
		Count:        inc.Count,
		FirstSeen:    inc.FirstSeen,
		LastSeen:     inc.LastSeen,
		Explanation:  inc.Explanation,
		ExplainErr:   inc.ExplainErr,
		Model:        inc.Model,
		Notification: string(kind),
		Pending:      inc.Explanation == nil && inc.ExplainErr == "",
	}
	if err := e.notifications.Emit(ctx, r); err != nil {
		e.log.Error("notification failed", "sink", e.notifications.Name(), "error", err)
		return
	}
	e.notificationsSent.Add(1)
}

// resolveLoop announces incidents that have gone quiet. It runs on its own
// ticker so resolution is not a function of incoming traffic.
func (e *Engine) resolveLoop(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, n := range e.notifier.Resolve(e.now()) {
				inc, ok := e.tracker.Lookup(n.Label, n.Fingerprint)
				if !ok {
					continue
				}
				e.push(ctx, inc, n.Kind)
			}
		}
	}
}

// resolveInterval chooses how often to scan for resolved incidents. Deriving it
// from W keeps a resolution prompt relative to the window itself; a test may
// override it to drive resolution directly.
func (e *Engine) resolveInterval() time.Duration {
	if e.resolveTick > 0 {
		return e.resolveTick
	}
	if e.resolveWindow <= 0 {
		return 0
	}
	tick := e.resolveWindow / 4
	if tick < minResolveTick {
		tick = minResolveTick
	}
	if tick > maxResolveTick {
		tick = maxResolveTick
	}
	return tick
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
