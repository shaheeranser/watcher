package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shaheeranser/watcher/internal/backend"
	"github.com/shaheeranser/watcher/internal/sink"
	"github.com/shaheeranser/watcher/internal/source"
)

type fakeSource struct {
	name  string
	lines []string
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Stream(ctx context.Context) (<-chan source.Line, error) {
	ch := make(chan source.Line)
	go func() {
		defer close(ch)
		for i, raw := range f.lines {
			select {
			case ch <- source.Line{Raw: raw, Source: f.name, ArrivedAt: time.Unix(int64(i), 0)}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

type blockingSource struct{ name string }

func (b *blockingSource) Name() string { return b.name }

func (b *blockingSource) Stream(ctx context.Context) (<-chan source.Line, error) {
	ch := make(chan source.Line)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}

type fakeBackend struct {
	mu    sync.Mutex
	calls int
	resp  backend.Explanation
	err   error
}

func (f *fakeBackend) Name() string { return "fake" }

func (f *fakeBackend) Explain(context.Context, backend.Request) (backend.Explanation, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return backend.Explanation{}, f.err
	}
	return f.resp, nil
}

func (f *fakeBackend) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type captureSink struct {
	mu      sync.Mutex
	results []sink.Result
}

func (c *captureSink) Name() string { return "capture" }

func (c *captureSink) Emit(_ context.Context, r sink.Result) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results = append(c.results, r)
	return nil
}

func (c *captureSink) all() []sink.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]sink.Result(nil), c.results...)
}

func validExplanation() backend.Explanation {
	return backend.Explanation{
		Summary:      "nil pointer dereference",
		LikelyCause:  "the handler dereferenced a nil request",
		Evidence:     []string{"panic: boom"},
		SuggestedFix: "guard the nil check",
		Confidence:   0.7,
		Severity:     "high",
	}
}

func baseOptions(src source.Source, be backend.Backend, snk sink.Sink) Options {
	return Options{
		Source:        src,
		Backend:       be,
		Sink:          snk,
		ContextBefore: 5,
		ContextAfter:  0,
		ContextBudget: 8192,
		MaxBlockLines: 200,
		Workers:       1,
		ExplainWindow: time.Minute,
		Buffer:        16,
	}
}

var goPanic = []string{
	"panic: boom",
	"goroutine 1 [running]:",
	"\t/app/main.go:10 +0x1a",
	"exit status 2",
}

func TestEndToEndJSONLShape(t *testing.T) {
	var buf bytes.Buffer
	be := &fakeBackend{resp: validExplanation()}
	eng := New(baseOptions(&fakeSource{name: "stdin", lines: goPanic}, be, sink.NewJSONL(&buf)))

	if err := eng.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("output lines = %d, want 1\n%s", len(lines), buf.String())
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	for _, key := range []string{"fingerprint", "kind", "source", "count", "first_seen", "last_seen",
		"summary", "likely_cause", "evidence", "suggested_fix", "confidence", "severity", "model"} {
		if _, ok := record[key]; !ok {
			t.Errorf("JSON output missing %q", key)
		}
	}
	if record["kind"] != "go-panic" {
		t.Errorf("kind = %v, want go-panic", record["kind"])
	}
	if record["count"].(float64) != 1 {
		t.Errorf("count = %v, want 1", record["count"])
	}
	if record["severity"] != "high" || record["model"] != "fake" {
		t.Errorf("unexpected severity/model: %v/%v", record["severity"], record["model"])
	}
}

func TestRepeatCrashCallsModelOnce(t *testing.T) {
	be := &fakeBackend{resp: validExplanation()}
	snk := &captureSink{}
	lines := append(append([]string{}, goPanic...), goPanic...)
	eng := New(baseOptions(&fakeSource{name: "app.log", lines: lines}, be, snk))

	if err := eng.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := be.callCount(); got != 1 {
		t.Errorf("backend calls = %d, want 1 for a repeated crash", got)
	}
	results := snk.all()
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Count != 1 || results[1].Count != 2 {
		t.Errorf("counts = %d, %d, want 1, 2", results[0].Count, results[1].Count)
	}
	if results[1].Explanation == nil {
		t.Error("a repeat should reuse the cached explanation")
	}
}

func TestBackendFailureStillReports(t *testing.T) {
	be := &fakeBackend{err: errors.New("connection refused")}
	snk := &captureSink{}
	eng := New(baseOptions(&fakeSource{name: "stdin", lines: goPanic}, be, snk))

	if err := eng.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := snk.all()
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Explanation != nil {
		t.Error("Explanation should be nil when the backend fails")
	}
	if !strings.Contains(results[0].ExplainErr, "connection refused") {
		t.Errorf("ExplainErr = %q", results[0].ExplainErr)
	}
}

func TestShutdownLeavesNoGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()

	eng := New(baseOptions(&blockingSource{name: "stdin"}, &fakeBackend{resp: validExplanation()}, &captureSink{}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("goroutine leak: before %d, after %d", before, after)
	}
}

type blockingSink struct{ release chan struct{} }

func (b *blockingSink) Name() string { return "blocking" }

func (b *blockingSink) Emit(ctx context.Context, _ sink.Result) error {
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestSlowSinkDropsAndCounts(t *testing.T) {
	lines := []string{}
	for i := 0; i < 6; i++ {
		lines = append(lines,
			"panic: failure",
			"goroutine 1 [running]:",
			"\t/app/main.go:10 +0x1a",
			"exit status 2",
		)
	}
	be := &fakeBackend{resp: validExplanation()}
	slow := &blockingSink{release: make(chan struct{})}
	opts := baseOptions(&fakeSource{name: "stdin", lines: lines}, be, slow)
	opts.Buffer = 1
	eng := New(opts)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for eng.Dropped() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	dropped := eng.Dropped()

	close(slow.release)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}

	if dropped == 0 {
		t.Error("expected the slow-sink policy to drop and count at least one result")
	}
}
