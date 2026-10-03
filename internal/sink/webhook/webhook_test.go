package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shaheeranser/watcher/internal/sink"
)

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not met before the deadline")
}

func newTestSink(t *testing.T, server *httptest.Server, opts Options) *Webhook {
	t.Helper()
	opts.URL = server.URL
	opts.Provider = "generic"
	if opts.HTTPClient == nil {
		opts.HTTPClient = server.Client()
	}
	if opts.Fallback == "" {
		opts.Fallback = filepath.Join(t.TempDir(), "undelivered.jsonl")
	}
	w, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestRetryDeliversAfterTransientFailures(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	wh := newTestSink(t, server, Options{
		Retries: 5, BackoffBase: time.Second, BackoffMax: 30 * time.Second,
		sleep: func(context.Context, time.Duration) bool { return true },
	})
	if err := wh.Emit(context.Background(), fixtureResult()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return attempts.Load() >= 3 })
}

func TestRetryUsesExponentialBackoffWithJitter(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	sleeps := make(chan time.Duration, 8)
	wh := newTestSink(t, server, Options{
		Retries: 3, BackoffBase: time.Second, BackoffMax: 30 * time.Second,
		sleep:  func(_ context.Context, d time.Duration) bool { sleeps <- d; return true },
		jitter: func() float64 { return 1 },
	})
	if err := wh.Emit(context.Background(), fixtureResult()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return attempts.Load() >= 3 })

	for i, want := range []time.Duration{time.Second, 2 * time.Second} {
		select {
		case got := <-sleeps:
			if got != want {
				t.Errorf("backoff delay[%d] = %s, want %s", i, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing backoff delay %d", i)
		}
	}
}

func TestRetryAfterHeaderIsHonoured(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sleeps := make(chan time.Duration, 8)
	wh := newTestSink(t, server, Options{
		Retries: 3, BackoffBase: time.Second, BackoffMax: 30 * time.Second,
		sleep:  func(_ context.Context, d time.Duration) bool { sleeps <- d; return true },
		jitter: func() float64 { return 1 },
	})
	if err := wh.Emit(context.Background(), fixtureResult()); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-sleeps:
		if got != 3*time.Second {
			t.Errorf("delay = %s, want the honoured Retry-After of 3s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no retry delay observed")
	}
}

func TestFallbackSpoolsUndeliveredNotification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	spoolPath := filepath.Join(t.TempDir(), "undelivered.jsonl")
	wh := newTestSink(t, server, Options{
		Retries: 2, BackoffBase: time.Millisecond, BackoffMax: time.Millisecond,
		Fallback: spoolPath,
		sleep:    func(context.Context, time.Duration) bool { return true },
	})
	if err := wh.Emit(context.Background(), fixtureResult()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		info, err := os.Stat(spoolPath)
		return err == nil && info.Size() > 0
	})

	raw, err := os.ReadFile(spoolPath)
	if err != nil {
		t.Fatal(err)
	}
	var entry genericPayload
	if err := json.Unmarshal(firstLine(raw), &entry); err != nil {
		t.Fatalf("spool line is not JSON: %v\n%s", err, raw)
	}
	if entry.Fingerprint != "abc123" {
		t.Errorf("spooled fingerprint = %q", entry.Fingerprint)
	}
	if entry.DeliveryError == "" {
		t.Error("the spooled notification must carry the failure reason")
	}
}

func TestEnqueueDropsOldestWhenQueueIsFull(t *testing.T) {
	w := &Webhook{queue: make(chan sink.Result, 1)}
	w.enqueue(sink.Result{Source: "a", Fingerprint: "1"})
	w.enqueue(sink.Result{Source: "b", Fingerprint: "2"})

	if got := w.Dropped(); got != 1 {
		t.Errorf("dropped = %d, want 1", got)
	}
	survivor := <-w.queue
	if survivor.Source != "b" {
		t.Errorf("survivor = %q, want the newest notification", survivor.Source)
	}
}

func firstLine(data []byte) []byte {
	for i, b := range data {
		if b == '\n' {
			return data[:i]
		}
	}
	return data
}
