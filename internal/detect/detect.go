// Package detect turns log lines into crash Events. It recognizes a small set
// of crash shapes and assembles each into the verbatim multi-line block that
// belongs to it, so nothing downstream has to re-parse the log.
package detect

import (
	"context"
	"time"

	"github.com/shaheeranser/watcher/internal/source"
)

// Kind identifies which pattern produced an event. The value is carried all the
// way to the fingerprint so that two crashes of different shapes never collide.
const (
	KindGoPanic         = "go-panic"
	KindPythonTraceback = "python-traceback"
	KindGenericFatal    = "generic-fatal"
)

// defaultIdle is how long a finished block waits for following context before
// it is emitted anyway. Without it a crash that is the last line on a source
// that never ends (a tailed file, a Docker container) would be held until the
// source produced something else.
const defaultIdle = time.Second

// Event is one detected crash: the line that triggered it, the full block that
// belongs to it, and a few following lines the context curator may use.
type Event struct {
	Kind       string
	Trigger    source.Line
	Block      []source.Line
	Tail       []source.Line
	DetectedAt time.Time
}

// Detector consumes Lines and emits Events. A line that matches no pattern
// produces no Event, so the stream is passed over rather than reported.
type Detector interface {
	Detect(ctx context.Context, in <-chan source.Line, out chan<- Event) error
	Name() string
}

type detector struct {
	tailLines     int
	maxBlockLines int
	idle          time.Duration
	now           func() time.Time
}

// New returns a Detector that keeps at most tailLines lines after each block
// and caps a block at maxBlockLines lines.
func New(tailLines, maxBlockLines int) *detector {
	return &detector{
		tailLines:     tailLines,
		maxBlockLines: maxBlockLines,
		idle:          defaultIdle,
		now:           time.Now,
	}
}

func (d *detector) Name() string { return "patterns" }

func (d *detector) Detect(ctx context.Context, in <-chan source.Line, out chan<- Event) error {
	c := newCollector(d.tailLines, d.maxBlockLines, d.now)
	timer := time.NewTimer(d.idle)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	// rearm keeps the idle timer running only while a finished block is waiting
	// for its tail: each following line postpones the flush, and a block still
	// being collected needs no timer.
	rearm := func() {
		if c.state == stateTail {
			resetTimer(timer, d.idle)
			return
		}
		stopTimer(timer)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case line, ok := <-in:
			if !ok {
				return c.flush(ctx, out)
			}
			if err := c.process(ctx, line, out); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			rearm()
		case <-timer.C:
			if err := c.flushIdle(ctx, out); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

func resetTimer(timer *time.Timer, d time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
