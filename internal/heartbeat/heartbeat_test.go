package heartbeat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

var fixed = time.Date(2026, 1, 2, 15, 6, 11, 0, time.UTC)

// recorder is an httptest receiver that captures every ping body.
type recorder struct {
	mu     sync.Mutex
	bodies [][]byte
	status int
}

func newRecorder(status int) *recorder { return &recorder{status: status} }

func (r *recorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.bodies = append(r.bodies, body)
		r.mu.Unlock()
		w.WriteHeader(r.status)
	}
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

func (r *recorder) first(t *testing.T) []byte {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.bodies) == 0 {
		t.Fatal("no heartbeat received")
	}
	return r.bodies[0]
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition was not met before the deadline")
}

func TestDisabledWithoutURL(t *testing.T) {
	h := New(Options{})
	if h.Enabled() {
		t.Fatal("a heartbeat with no URL must be disabled")
	}

	done := make(chan struct{})
	go func() {
		h.Run(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run must return immediately when no URL is configured")
	}
}

func TestPingsOnItsOwnSchedule(t *testing.T) {
	server := newRecorder(http.StatusNoContent)
	ts := httptest.NewServer(server.handler())
	defer ts.Close()

	ticks := make(chan time.Time, 4)
	h := New(Options{
		URL:        ts.URL,
		Interval:   time.Minute,
		HTTPClient: ts.Client(),
		tick:       func(time.Duration) <-chan time.Time { return ticks },
		now:        func() time.Time { return fixed },
		Stats:      func() Stats { return Stats{LinesProcessed: 42} },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		h.Run(ctx)
		close(done)
	}()

	// Two ticks with no incident traffic in between still produce two pings:
	// the cadence is the heartbeat's own (PROD-HB-3).
	ticks <- fixed
	waitFor(t, func() bool { return server.count() >= 1 })
	ticks <- fixed.Add(time.Minute)
	waitFor(t, func() bool { return server.count() >= 2 })

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestPayloadCarriesTheLivenessCounters(t *testing.T) {
	server := newRecorder(http.StatusNoContent)
	ts := httptest.NewServer(server.handler())
	defer ts.Close()

	h := New(Options{
		URL:        ts.URL,
		Interval:   time.Minute,
		HTTPClient: ts.Client(),
		now:        func() time.Time { return fixed },
		Stats: func() Stats {
			return Stats{
				Uptime:               3600 * time.Second,
				LinesProcessed:       128400,
				IncidentsTracked:     7,
				NotificationsSent:    19,
				NotificationsDropped: 2,
			}
		},
	})
	h.ping(context.Background())

	var p map[string]any
	if err := json.Unmarshal(server.first(t), &p); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"timestamp", "uptime_seconds", "lines_processed", "incidents_tracked", "notifications_sent", "notifications_dropped"} {
		if _, ok := p[key]; !ok {
			t.Errorf("heartbeat payload missing %q", key)
		}
	}
	if p["timestamp"] != "2026-01-02T15:06:11Z" {
		t.Errorf("timestamp = %v, want RFC3339 UTC", p["timestamp"])
	}
	if p["uptime_seconds"].(float64) != 3600 || p["lines_processed"].(float64) != 128400 {
		t.Errorf("unexpected counters: %v", p)
	}
	if p["incidents_tracked"].(float64) != 7 || p["notifications_sent"].(float64) != 19 || p["notifications_dropped"].(float64) != 2 {
		t.Errorf("unexpected counters: %v", p)
	}
}

func TestFailureIsLoggedAndContinues(t *testing.T) {
	server := newRecorder(http.StatusInternalServerError)
	ts := httptest.NewServer(server.handler())
	defer ts.Close()

	ticks := make(chan time.Time, 4)
	h := New(Options{
		URL:        ts.URL,
		Interval:   time.Minute,
		HTTPClient: ts.Client(),
		tick:       func(time.Duration) <-chan time.Time { return ticks },
		now:        func() time.Time { return fixed },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)

	// A rejected ping does not stop the loop; the next tick still fires
	// (PROD-HB-4).
	ticks <- fixed
	ticks <- fixed.Add(time.Minute)
	waitFor(t, func() bool { return server.count() >= 2 })
	cancel()
}

func TestUnreachableURLIsNotFatal(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	url := ts.URL
	ts.Close() // nothing is listening now

	ticks := make(chan time.Time, 2)
	h := New(Options{
		URL:      url,
		Interval: time.Minute,
		tick:     func(time.Duration) <-chan time.Time { return ticks },
		now:      func() time.Time { return fixed },
		// A failed dial must return rather than hang.
		HTTPClient: &http.Client{Timeout: 200 * time.Millisecond},
	})

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		h.Run(ctx)
		close(done)
	}()

	ticks <- fixed
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a failed heartbeat must not block Run")
	}
}
