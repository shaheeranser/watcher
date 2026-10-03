// Package webhook delivers incident notifications to an HTTP endpoint. It
// builds a provider-specific payload (generic JSON, Slack, or Discord), retries
// with exponential backoff and jitter, and spools anything it cannot deliver to
// a local file. Delivery is asynchronous and bounded, so a wedged receiver can
// never stall detection.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shaheeranser/watcher/internal/sink"
)

const defaultQueueSize = 256

// Options configures a webhook sink. The unexported fields are test seams: a
// fake clock, a recording sleep, and a deterministic jitter.
type Options struct {
	URL         string
	Provider    string
	Retries     int
	BackoffBase time.Duration
	BackoffMax  time.Duration
	Fallback    string
	QueueSize   int
	Logger      *slog.Logger
	HTTPClient  *http.Client

	now    func() time.Time
	sleep  func(context.Context, time.Duration) bool
	jitter func() float64
}

// Webhook is a bounded, asynchronous webhook sink. Emit never blocks: it
// enqueues. Notification policy (what is worth sending, and when) is decided
// upstream by the incident state machine, so this sink no longer throttles.
type Webhook struct {
	url     string
	build   payloadBuilder
	retries int
	base    time.Duration
	max     time.Duration
	spool   *spool
	queue   chan sink.Result
	client  *http.Client
	log     *slog.Logger

	now    func() time.Time
	sleep  func(context.Context, time.Duration) bool
	jitter func() float64

	mu      sync.Mutex
	dropped atomic.Int64
}

// New builds the sink and starts its delivery goroutine. The worker stops when
// ctx is cancelled.
func New(ctx context.Context, opts Options) (*Webhook, error) {
	if opts.URL == "" {
		return nil, errors.New("webhook URL is empty")
	}
	build, err := builderFor(opts.Provider)
	if err != nil {
		return nil, err
	}

	w := &Webhook{
		url:     opts.URL,
		build:   build,
		retries: positiveOr(opts.Retries, 5),
		base:    durationOr(opts.BackoffBase, time.Second),
		max:     durationOr(opts.BackoffMax, 30*time.Second),
		queue:   make(chan sink.Result, positiveOr(opts.QueueSize, defaultQueueSize)),
		client:  opts.HTTPClient,
		log:     opts.Logger,
		now:     timeOr(opts.now, time.Now),
		sleep:   sleepOr(opts.sleep),
		jitter:  floatOr(opts.jitter, rand.Float64),
	}
	if w.client == nil {
		w.client = &http.Client{}
	}
	if w.log == nil {
		w.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.Fallback != "" {
		w.spool = newSpool(opts.Fallback)
	}

	go w.run(ctx)
	return w, nil
}

func (w *Webhook) Name() string { return "webhook" }

// Emit enqueues the notification. It reports no error for a dropped
// notification, because a full queue is not a failure of the caller.
func (w *Webhook) Emit(_ context.Context, r sink.Result) error {
	w.enqueue(r)
	return nil
}

// Dropped reports how many notifications were discarded because the queue was
// full.
func (w *Webhook) Dropped() int64 { return w.dropped.Load() }

// enqueue adds to the bounded queue, dropping the oldest and counting it when
// full so a slow receiver cannot grow the process (RT-WH-7).
func (w *Webhook) enqueue(r sink.Result) {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case w.queue <- r:
		return
	default:
	}

	select {
	case <-w.queue:
		w.dropped.Add(1)
	default:
	}
	select {
	case w.queue <- r:
	default:
		w.dropped.Add(1)
	}
}

func (w *Webhook) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-w.queue:
			w.deliver(ctx, r)
		}
	}
}

// deliver builds the payload and retries until it is accepted or the attempts
// are exhausted, then spools the notification.
func (w *Webhook) deliver(ctx context.Context, r sink.Result) {
	body, err := json.Marshal(w.build(r))
	if err != nil {
		w.log.Error("cannot build webhook payload", "error", err)
		return
	}

	var lastErr error
	var retryAfter time.Duration
	for attempt := 1; attempt <= w.retries; attempt++ {
		if attempt > 1 {
			delay := w.backoffDelay(attempt)
			if retryAfter > delay {
				delay = retryAfter
			}
			if !w.sleep(ctx, delay) {
				return
			}
			retryAfter = 0
		}

		if retryAfter, lastErr = w.post(ctx, body); lastErr == nil {
			return
		}
		w.log.Warn("webhook delivery failed", "attempt", attempt, "error", lastErr)
		if ctx.Err() != nil {
			return
		}
	}

	if w.spool == nil {
		w.log.Error("webhook delivery failed with no fallback file", "error", lastErr)
		return
	}
	if err := w.spool.append(r, lastErr); err != nil {
		w.log.Error("cannot spool undelivered notification", "error", err)
	}
}

// post sends one attempt and reports how long to wait before retrying. A nil
// error means the receiver accepted the notification.
func (w *Webhook) post(ctx context.Context, body []byte) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return 0, nil
	}
	return parseRetryAfter(resp.Header.Get("Retry-After"), w.now()), fmt.Errorf("webhook status %d", resp.StatusCode)
}

// backoffDelay is min(cap, base·2^(attempt-2)) scaled by jitter in [0.5, 1.0].
func (w *Webhook) backoffDelay(attempt int) time.Duration {
	if attempt <= 1 || w.base <= 0 {
		return 0
	}
	delay := w.base
	for i := 2; i < attempt; i++ {
		if delay >= w.max {
			break
		}
		delay *= 2
	}
	if delay > w.max {
		delay = w.max
	}

	factor := w.jitter()
	if factor < 0.5 {
		factor = 0.5
	}
	if factor > 1 {
		factor = 1
	}
	return time.Duration(float64(delay) * factor)
}

// parseRetryAfter reads the header in either of its forms: a number of seconds
// or an HTTP date.
func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := when.Sub(now); delay > 0 {
			return delay
		}
	}
	return 0
}

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

func positiveOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func durationOr(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func timeOr(fn func() time.Time, fallback func() time.Time) func() time.Time {
	if fn != nil {
		return fn
	}
	return fallback
}

func floatOr(fn func() float64, fallback func() float64) func() float64 {
	if fn != nil {
		return fn
	}
	return fallback
}

func sleepOr(fn func(context.Context, time.Duration) bool) func(context.Context, time.Duration) bool {
	if fn != nil {
		return fn
	}
	return sleepCtx
}
