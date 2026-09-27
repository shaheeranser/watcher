package source

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"time"
)

// stdinPollInterval is how often a blocked stdin read is interrupted so that
// context cancellation is noticed even when no more input arrives.
const stdinPollInterval = 250 * time.Millisecond

// Stdin streams lines from a reader assumed to be standard input. Because the
// reader is injected, tests can drive it without touching the real stdin.
type Stdin struct {
	r       io.Reader
	bufSize int
	log     *slog.Logger
}

func NewStdin(r io.Reader, log *slog.Logger) *Stdin {
	return &Stdin{r: r, bufSize: defaultBuffer, log: loggerOrDiscard(log)}
}

func (s *Stdin) Name() string { return "stdin" }

// Stream emits each line as soon as it is read, without waiting for EOF
// (CORE-SRC-1). The final unterminated line is flushed at EOF, since a closed
// stream means no writer remains to complete it (CORE-SRC-6).
func (s *Stdin) Stream(ctx context.Context) (<-chan Line, error) {
	out := make(chan Line, s.bufSize)
	go func() {
		defer close(out)
		acc := newLineAccumulator(s.r)
		for {
			if ctx.Err() != nil {
				return
			}
			setReadDeadline(s.r, stdinPollInterval)
			lines, err := acc.read()
			for _, raw := range lines {
				if !sendLine(ctx, out, Line{Raw: raw, Source: "stdin", ArrivedAt: time.Now()}) {
					return
				}
			}

			switch {
			case err == nil:
			case errors.Is(err, io.EOF):
				if raw, ok := acc.takePending(); ok {
					sendLine(ctx, out, Line{Raw: raw, Source: "stdin", ArrivedAt: time.Now()})
				}
				return
			case errors.Is(err, os.ErrDeadlineExceeded):
				// Idle: no data yet. Loop so cancellation stays responsive.
			default:
				s.log.Error("stdin read failed", "error", err)
				return
			}
		}
	}()
	return out, nil
}
