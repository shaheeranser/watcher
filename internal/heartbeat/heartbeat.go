// Package heartbeat sends a periodic liveness ping to a dead-man's-switch URL,
// so Watcher's own silence — a crash, a lost network — is itself detectable
// from outside. It runs on its own schedule, independent of incident traffic,
// and never retries: the absence of the next ping is the signal, so a failed
// delivery is logged and dropped rather than retried into a backlog.
package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// defaultInterval stands in when a caller never set one. The configured value
// is validated positive at startup; this only keeps the type safe to use.
const defaultInterval = time.Minute

// Stats is the liveness snapshot a ping carries: enough to tell "alive and
// idle" from "dead" (PROD-HB-2).
type Stats struct {
	Uptime               time.Duration
	LinesProcessed       int64
	IncidentsTracked     int
	NotificationsSent    int64
	NotificationsDropped int64
}

// Options configures the heartbeat. URL is the dead-man's-switch endpoint;
// without one the heartbeat is disabled. Stats and the unexported seams let a
// test control the payload and the schedule.
type Options struct {
	URL      string
	Interval time.Duration
	Stats    func() Stats
	Logger   *slog.Logger

	// HTTPClient is optional; a bounded default is used when nil.
	HTTPClient *http.Client

	now  func() time.Time
	tick func(time.Duration) <-chan time.Time
}

// Heartbeat posts a JSON payload to its URL on a fixed interval.
type Heartbeat struct {
	url      string
	interval time.Duration
	stats    func() Stats
	log      *slog.Logger
	client   *http.Client

	now  func() time.Time
	tick func(time.Duration) <-chan time.Time
}

// New builds a heartbeat. It is disabled unless a URL is configured, in which
// case Run returns immediately (PROD-HB-5).
func New(opts Options) *Heartbeat {
	interval := opts.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	stats := opts.Stats
	if stats == nil {
		stats = func() Stats { return Stats{} }
	}
	now := opts.now
	if now == nil {
		now = time.Now
	}
	return &Heartbeat{
		url:      opts.URL,
		interval: interval,
		stats:    stats,
		log:      log,
		client:   client,
		now:      now,
		tick:     opts.tick,
	}
}

// Enabled reports whether a heartbeat URL is configured.
func (h *Heartbeat) Enabled() bool { return h.url != "" }

// Run pings until the context is cancelled. It owns its own ticker, so the
// heartbeat cadence never depends on incident traffic (PROD-HB-3).
func (h *Heartbeat) Run(ctx context.Context) {
	if !h.Enabled() {
		return
	}
	if h.tick != nil {
		h.loop(ctx, h.tick(h.interval))
		return
	}
	t := time.NewTicker(h.interval)
	defer t.Stop()
	h.loop(ctx, t.C)
}

func (h *Heartbeat) loop(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			h.ping(ctx)
		}
	}
}

// ping sends one heartbeat. A failure is logged and forgotten; the next ping is
// the liveness signal, so there is nothing to retry.
func (h *Heartbeat) ping(ctx context.Context) {
	body, err := json.Marshal(payloadFrom(h.stats(), h.now()))
	if err != nil {
		h.log.Error("cannot build heartbeat payload", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		h.log.Error("cannot build heartbeat request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		h.log.Warn("heartbeat delivery failed", "error", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		h.log.Warn("heartbeat rejected", "status", resp.StatusCode)
	}
}

// payload is the wire shape of one ping.
type payload struct {
	Timestamp            string `json:"timestamp"`
	UptimeSeconds        int64  `json:"uptime_seconds"`
	LinesProcessed       int64  `json:"lines_processed"`
	IncidentsTracked     int    `json:"incidents_tracked"`
	NotificationsSent    int64  `json:"notifications_sent"`
	NotificationsDropped int64  `json:"notifications_dropped"`
}

func payloadFrom(s Stats, now time.Time) payload {
	return payload{
		Timestamp:            now.UTC().Format(time.RFC3339),
		UptimeSeconds:        int64(s.Uptime.Seconds()),
		LinesProcessed:       s.LinesProcessed,
		IncidentsTracked:     s.IncidentsTracked,
		NotificationsSent:    s.NotificationsSent,
		NotificationsDropped: s.NotificationsDropped,
	}
}
