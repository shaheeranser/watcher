package detect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shaheeranser/watcher/internal/source"
)

// verdict is what the continuation predicate decides about one line while a
// block is being collected.
type verdict int

const (
	// vAccept includes the line and keeps collecting.
	vAccept verdict = iota
	// vAcceptEnd includes the line and finishes the block.
	vAcceptEnd
	// vReject excludes the line and finishes the block.
	vReject
)

type collectorState int

const (
	stateIdle collectorState = iota
	stateBlock
	stateTail
)

// collector is the per-event assembly state machine: idle until a trigger,
// collecting while the predicate holds, then gathering a bounded tail before
// the event is handed on.
//
// A block is stored as a fixed head plus a rolling tail. That shape is what
// lets an over-long block be capped without ever losing the trigger (in the
// head) or the root cause and innermost frames (in the tail), per CORE-DET-6.
type collector struct {
	tailLines     int
	maxBlockLines int
	headKeep      int
	tailKeep      int
	now           func() time.Time

	state               collectorState
	event               Event
	seenGoroutineHeader bool
	tailLeft            int

	head     []source.Line
	tail     []source.Line
	elided   int
	overflow bool
}

func newCollector(tailLines, maxBlockLines int, now func() time.Time) *collector {
	headKeep := maxBlockLines / 2
	return &collector{
		tailLines:     tailLines,
		maxBlockLines: maxBlockLines,
		headKeep:      headKeep,
		tailKeep:      maxBlockLines - 1 - headKeep,
		now:           now,
	}
}

func (c *collector) process(ctx context.Context, line source.Line, out chan<- Event) error {
	switch c.state {
	case stateIdle:
		if kind, ok := TriggerKind(line.Raw); ok {
			c.start(kind, line)
		}
		return nil

	case stateBlock:
		return c.extend(ctx, line, out)

	default: // stateTail
		if kind, ok := TriggerKind(line.Raw); ok {
			if err := c.emit(ctx, out); err != nil {
				return err
			}
			c.start(kind, line)
			return nil
		}
		c.appendTail(line)
		if c.tailLeft <= 0 {
			return c.emit(ctx, out)
		}
		return nil
	}
}

func (c *collector) start(kind string, line source.Line) {
	c.event = Event{
		Kind:       kind,
		Trigger:    line,
		DetectedAt: c.now(),
	}
	c.state = stateBlock
	c.seenGoroutineHeader = false
	c.tailLeft = 0
	c.head = []source.Line{line}
	c.tail = nil
	c.elided = 0
	c.overflow = false
}

func (c *collector) extend(ctx context.Context, line source.Line, out chan<- Event) error {
	switch c.verdict(line.Raw) {
	case vAccept:
		c.appendBlock(line)
	case vAcceptEnd:
		c.appendBlock(line)
		return c.complete(ctx, out)
	case vReject:
		if err := c.complete(ctx, out); err != nil {
			return err
		}
		// The rejecting line may itself begin the next crash.
		return c.process(ctx, line, out)
	}
	return nil
}

func (c *collector) complete(ctx context.Context, out chan<- Event) error {
	if c.tailLines <= 0 {
		return c.emit(ctx, out)
	}
	c.state = stateTail
	c.tailLeft = c.tailLines
	return nil
}

func (c *collector) flush(ctx context.Context, out chan<- Event) error {
	if c.state == stateIdle {
		return nil
	}
	return c.emit(ctx, out)
}

// flushIdle emits a block whose tail has gone quiet, so a crash that is the
// last line on a source that never ends is still reported rather than held
// forever. It applies only to the tail state: a block that is still being
// collected is left alone, because a trigger line may still be arriving.
func (c *collector) flushIdle(ctx context.Context, out chan<- Event) error {
	if c.state != stateTail {
		return nil
	}
	return c.emit(ctx, out)
}

func (c *collector) emit(ctx context.Context, out chan<- Event) error {
	c.event.Block = c.block()
	event := c.event
	c.event = Event{}
	c.state = stateIdle
	c.tailLeft = 0
	select {
	case out <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *collector) appendBlock(line source.Line) {
	if !c.overflow {
		c.head = append(c.head, line)
		if len(c.head) <= c.maxBlockLines {
			return
		}
		// One line past capacity: split what we hold into a fixed head and a
		// rolling tail, then keep only the tail moving forward.
		all := c.head
		c.head = append([]source.Line(nil), all[:c.headKeep]...)
		c.tail = append([]source.Line(nil), all[len(all)-c.tailKeep:]...)
		c.elided = len(all) - c.headKeep - c.tailKeep
		c.overflow = true
		return
	}

	if len(c.tail) == c.tailKeep {
		copy(c.tail, c.tail[1:])
		c.tail = c.tail[:c.tailKeep-1]
		c.elided++
	}
	c.tail = append(c.tail, line)
}

func (c *collector) appendTail(line source.Line) {
	c.event.Tail = append(c.event.Tail, line)
	c.tailLeft--
}

func (c *collector) block() []source.Line {
	if !c.overflow {
		return c.head
	}
	last := c.tail[len(c.tail)-1]
	marker := source.Line{
		Raw:       fmt.Sprintf("... %d lines elided ...", c.elided),
		Source:    last.Source,
		ArrivedAt: last.ArrivedAt,
	}
	out := make([]source.Line, 0, c.maxBlockLines)
	out = append(out, c.head...)
	out = append(out, marker)
	out = append(out, c.tail...)
	return out
}

func (c *collector) verdict(raw string) verdict {
	// A new crash starting mid-block ends the current one. The python kind is
	// exempt because its terminator line legitimately matches the generic
	// trigger, and that line belongs to the traceback.
	if c.event.Kind != KindPythonTraceback {
		if _, ok := TriggerKind(raw); ok {
			return vReject
		}
	}

	switch c.event.Kind {
	case KindGoPanic:
		return c.verdictGoPanic(raw)
	case KindPythonTraceback:
		return verdictPython(raw)
	default:
		return verdictGeneric(raw)
	}
}

// verdictGoPanic follows the shape the Go runtime prints: an optional message
// and signal line, the goroutine header, then alternating function and
// file:line frames, optionally closed by a go-run exit status.
func (c *collector) verdictGoPanic(raw string) verdict {
	if !c.seenGoroutineHeader {
		if goGoroutineHeader.MatchString(raw) {
			c.seenGoroutineHeader = true
		}
		return vAccept
	}
	switch {
	case strings.HasPrefix(raw, "\t"), goFrame.MatchString(raw):
		return vAccept
	case goGoroutineHeader.MatchString(raw):
		return vAccept
	case exitStatus.MatchString(raw):
		return vAcceptEnd
	case raw == "":
		return vReject
	case newRecord.MatchString(raw):
		return vReject
	default:
		return vAccept
	}
}

// verdictPython ends on the first unindented exception line, which is the line
// carrying the root cause and therefore must be included.
func verdictPython(raw string) verdict {
	switch {
	case raw == "":
		return vReject
	case pythonException.MatchString(raw) && !startsWithSpace(raw):
		return vAcceptEnd
	case startsWithSpace(raw):
		return vAccept
	default:
		return vReject
	}
}

// verdictGeneric keeps only indented continuation lines of a fatal message and
// stops at the next log record.
func verdictGeneric(raw string) verdict {
	switch {
	case raw == "":
		return vReject
	case newRecord.MatchString(raw):
		return vReject
	case startsWithSpace(raw), continuationPhrase.MatchString(raw):
		return vAccept
	default:
		return vReject
	}
}

func startsWithSpace(s string) bool {
	return strings.HasPrefix(s, " ") || strings.HasPrefix(s, "\t")
}
