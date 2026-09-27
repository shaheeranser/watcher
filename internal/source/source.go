// Package source produces log lines from an input stream. A Source knows only
// how to deliver Lines; detection, fingerprinting, and reporting all happen
// downstream, so a new input kind never has to know about them.
package source

import (
	"context"
	"io"
	"log/slog"
	"time"
)

// defaultBuffer bounds how far a source may run ahead of its consumer. A full
// buffer stalls the read loop, which is the backpressure that keeps memory
// bounded when detection is slower than the input (CORE-SRC-4).
const defaultBuffer = 1024

// Line is one line of log text with the identity of the stream it came from.
type Line struct {
	Raw       string
	Source    string
	ArrivedAt time.Time
}

// Source streams Lines until the context is cancelled or the input ends, then
// closes the channel. Stream returns an error only for setup failures; a
// mid-stream failure is logged and ends the stream rather than being returned.
type Source interface {
	Stream(ctx context.Context) (<-chan Line, error)
	Name() string
}

func loggerOrDiscard(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return l
}

// sendLine delivers a line unless the context is cancelled first. It reports
// whether the line was delivered, letting callers stop promptly on shutdown.
func sendLine(ctx context.Context, out chan<- Line, line Line) bool {
	select {
	case out <- line:
		return true
	case <-ctx.Done():
		return false
	}
}

// sleepCtx waits for d, returning false as soon as ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// setReadDeadline arms a read timeout when the reader supports one. It is what
// makes a blocked read wake periodically so cancellation is observed; readers
// that never block (regular files, bytes.Reader) simply ignore it.
func setReadDeadline(r io.Reader, d time.Duration) {
	if f, ok := r.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = f.SetReadDeadline(time.Now().Add(d))
	}
}
