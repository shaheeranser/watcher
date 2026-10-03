package tui

import (
	"context"
	"errors"
	"time"

	"github.com/shaheeranser/watcher/internal/store"
)

const (
	minBackoff = 500 * time.Millisecond
	maxBackoff = 10 * time.Second

	// watchdogFactor scales the SSE watchdog off the refresh interval: when no
	// mutation arrives within this many intervals the source polls instead, so a
	// stalled stream never freezes the UI silently (DASH-21).
	watchdogFactor = 5
)

// nextResult is one read from the event stream: either an event or the error
// that ended the stream.
type nextResult struct {
	ev  store.Event
	err error
}

// RunSource feeds the program from the daemon until ctx is cancelled. SSE is the
// primary channel; a per-mutation event triggers a refetch, and a watchdog
// falls back to periodic polling when the stream goes quiet (DASH-19, DASH-21).
// A dropped stream is reported as reconnecting and reopened with backoff
// (DASH-9).
func RunSource(ctx context.Context, open OpenFunc, refresh time.Duration, send sendFunc) {
	if refresh <= 0 {
		refresh = 2 * time.Second
	}
	watchdog := refresh * watchdogFactor
	if watchdog < time.Second {
		watchdog = time.Second
	}

	backoff := minBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		reader, err := open(ctx)
		if err != nil {
			send(connMsg{state: ConnReconnecting, err: err})
			if !sleep(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		backoff = minBackoff
		send(connMsg{state: ConnConnected})
		ended := readStream(ctx, reader, watchdog, send)
		reader.Close()
		if ctx.Err() != nil {
			return
		}
		if ended {
			send(connMsg{state: ConnReconnecting, err: errors.New("event stream closed")})
			if !sleep(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
		}
	}
}

// readStream forwards messages until the stream ends or ctx is cancelled. It
// returns true when the stream ended on its own (a disconnect to reconnect
// from) and false when the cancellation stopped it.
func readStream(ctx context.Context, reader EventReader, watchdog time.Duration, send sendFunc) bool {
	results := make(chan nextResult, 1)
	go func() {
		for {
			ev, err := reader.Next()
			select {
			case results <- nextResult{ev: ev, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	timer := time.NewTimer(watchdog)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			send(pollMsg{})
			timer.Reset(watchdog)
		case result := <-results:
			if result.err != nil {
				return true
			}
			send(eventMsg{event: result.ev})
			timer.Reset(watchdog)
		}
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
