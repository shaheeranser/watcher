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
	"github.com/shaheeranser/watcher/internal/store"
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

type failingSource struct{ name string }

func (f *failingSource) Name() string { return f.name }

func (f *failingSource) Stream(context.Context) (<-chan source.Line, error) {
	return nil, errors.New("cannot attach to source")
}

type fakeBackend struct {
	mu    sync.Mutex
	calls int
	resp  backend.Explanation
	err   error
}

func (f *fakeBackend) Name() string { return "fake" }

func (f *fakeBackend) Model() string { return "fake-model" }

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
		Sources:       []source.Source{src},
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

func TestRunIDEchoedOnLocalResults(t *testing.T) {
	be := &fakeBackend{resp: validExplanation()}
	snk := &captureSink{}
	opts := baseOptions(&fakeSource{name: "stdin", lines: goPanic}, be, snk)
	opts.RunID = "run-014"

	if err := New(opts).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	results := snk.all()
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].RunID != "run-014" {
		t.Errorf("result run id = %q, want run-014", results[0].RunID)
	}
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
	if record["severity"] != "high" || record["model"] != "fake-model" {
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

var pythonTraceback = []string{
	"Traceback (most recent call last):",
	`  File "/app/main.py", line 3, in <module>`,
	"    main()",
	`  File "/app/main.py", line 1, in main`,
	`    raise ValueError("boom")`,
	"ValueError: boom",
}

func TestSourcesDoNotCrossContaminate(t *testing.T) {
	be := &fakeBackend{resp: validExplanation()}
	snk := &captureSink{}
	opts := baseOptions(&fakeSource{name: "x", lines: goPanic}, be, snk)
	opts.Sources = []source.Source{
		&fakeSource{name: "backend", lines: goPanic},
		&fakeSource{name: "worker", lines: pythonTraceback},
	}

	if err := New(opts).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := snk.all()
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2: %+v", len(results), results)
	}
	bySource := make(map[string]sink.Result, len(results))
	for _, r := range results {
		bySource[r.Source] = r
	}
	if r, ok := bySource["backend"]; !ok || r.Kind != "go-panic" {
		t.Errorf("backend result = %+v", r)
	}
	if r, ok := bySource["worker"]; !ok || r.Kind != "python-traceback" {
		t.Errorf("worker result = %+v", r)
	}
}

func TestSameCrashOnTwoLabelsIsTwoIncidents(t *testing.T) {
	be := &fakeBackend{resp: validExplanation()}
	snk := &captureSink{}
	opts := baseOptions(&fakeSource{name: "x", lines: goPanic}, be, snk)
	opts.Sources = []source.Source{
		&fakeSource{name: "backend", lines: goPanic},
		&fakeSource{name: "frontend", lines: goPanic},
	}

	if err := New(opts).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := snk.all()
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2 (same crash, two labels)", len(results))
	}
	seen := make(map[string]bool)
	hash := results[0].Fingerprint
	for _, r := range results {
		if r.Count != 1 {
			t.Errorf("%s count = %d, want 1", r.Source, r.Count)
		}
		if r.Fingerprint != hash {
			t.Errorf("fingerprint %q differs across labels; the hash must not depend on the label", r.Fingerprint)
		}
		seen[r.Source] = true
	}
	if !seen["backend"] || !seen["frontend"] {
		t.Errorf("labels seen = %v, want backend and frontend", seen)
	}
	if got := be.callCount(); got != 2 {
		t.Errorf("backend calls = %d, want 2 (one per label)", got)
	}
}

func TestOneSourceEndingKeepsOthersRunning(t *testing.T) {
	be := &fakeBackend{resp: validExplanation()}
	snk := &captureSink{}
	opts := baseOptions(&fakeSource{name: "x", lines: goPanic}, be, snk)
	opts.Sources = []source.Source{
		&fakeSource{name: "app-a.log", lines: goPanic},
		&blockingSource{name: "app-b.log"},
	}
	eng := New(opts)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for len(snk.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(snk.all()) == 0 {
		t.Fatal("no result arrived from the source that ended")
	}
	select {
	case <-done:
		t.Fatal("Run returned while another source was still active")
	default:
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestSourceSetupErrorIsFatal(t *testing.T) {
	eng := New(baseOptions(&failingSource{name: "broken"}, &fakeBackend{resp: validExplanation()}, &captureSink{}))
	err := eng.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("error = %v, want a fatal setup error naming the source", err)
	}
}

// fakeClock is a concurrency-safe clock the state-machine tests advance by hand.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// chanSource streams lines fed to it on demand, so a test can drive occurrences
// and resolutions in a fixed order.
type chanSource struct {
	name  string
	lines chan string
}

func (c *chanSource) Name() string { return c.name }

func (c *chanSource) Stream(ctx context.Context) (<-chan source.Line, error) {
	out := make(chan source.Line, 16)
	go func() {
		defer close(out)
		i := 0
		for {
			select {
			case <-ctx.Done():
				return
			case raw, ok := <-c.lines:
				if !ok {
					return
				}
				select {
				case out <- source.Line{Raw: raw, Source: c.name, ArrivedAt: time.Unix(int64(i), 0)}:
				case <-ctx.Done():
					return
				}
				i++
			}
		}
	}()
	return out, nil
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

func send(s *chanSource, lines []string) {
	for _, l := range lines {
		s.lines <- l
	}
}

func notificationKinds(results []sink.Result) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Notification
	}
	return out
}

func TestNotificationsAnnounceNewThenExplanation(t *testing.T) {
	be := &fakeBackend{resp: validExplanation()}
	local := &captureSink{}
	notify := &captureSink{}
	src := &chanSource{name: "app.log", lines: make(chan string, 16)}

	opts := baseOptions(src, be, local)
	opts.Notifications = notify
	opts.ThrottleWindow = 15 * time.Minute
	opts.ResolveWindow = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	eng := New(opts)
	go func() { done <- eng.Run(ctx) }()

	send(src, goPanic)

	waitFor(t, func() bool { return len(notify.all()) >= 2 })
	kinds := notificationKinds(notify.all())
	if kinds[0] != "new" || kinds[1] != "ongoing" {
		t.Fatalf("notification kinds = %v, want [new ongoing]", kinds)
	}
	first := notify.all()[0]
	if !first.Pending {
		t.Error("the new notification should be pending, having gone out before the model call")
	}
	if first.Explanation != nil {
		t.Error("the new notification must not carry an explanation")
	}
	if second := notify.all()[1]; second.Explanation == nil {
		t.Error("the follow-up notification should carry the explanation")
	}

	// A repeat inside the throttle window is counted locally but not notified.
	send(src, goPanic)
	waitFor(t, func() bool { return len(local.all()) >= 2 })
	if got := len(notify.all()); got != 2 {
		t.Errorf("notifications = %d, want 2 (the repeat is suppressed)", got)
	}
	if got := local.all()[1].Count; got != 2 {
		t.Errorf("local count = %d, want 2 (every occurrence is still reported)", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
}

func TestNotificationResolvesAndReopensFresh(t *testing.T) {
	be := &fakeBackend{resp: validExplanation()}
	notify := &captureSink{}
	clock := &fakeClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	src := &chanSource{name: "app.log", lines: make(chan string, 16)}

	opts := baseOptions(src, be, &captureSink{})
	opts.Notifications = notify
	opts.ThrottleWindow = time.Minute
	opts.ResolveWindow = 10 * time.Second
	opts.resolveTick = 2 * time.Millisecond
	opts.now = clock.Now

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	eng := New(opts)
	go func() { done <- eng.Run(ctx) }()

	send(src, goPanic)
	waitFor(t, func() bool { return hasKind(notify.all(), "new") })

	// Quiet past the resolve window: exactly one resolved is announced.
	clock.Advance(11 * time.Second)
	waitFor(t, func() bool { return hasKind(notify.all(), "resolved") })

	// A recurrence after resolution starts a fresh cycle with a reset count.
	clock.Advance(time.Second)
	send(src, goPanic)
	waitFor(t, func() bool { return countNew(notify.all()) == 2 })
	for _, r := range notify.all() {
		if r.Notification == "new" && r.Count != 1 {
			t.Errorf("reopened new notification count = %d, want 1", r.Count)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
}

func hasKind(results []sink.Result, kind string) bool {
	for _, r := range results {
		if r.Notification == kind {
			return true
		}
	}
	return false
}

func countNew(results []sink.Result) int {
	n := 0
	for _, r := range results {
		if r.Notification == "new" {
			n++
		}
	}
	return n
}

// fakeRecorder captures the persistence projection the engine emits.
type fakeRecorder struct {
	mu       sync.Mutex
	upserts  []store.Upsert
	explains []store.Explain
	resolves []store.Resolve
}

func (f *fakeRecorder) Observe(u store.Upsert) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upserts = append(f.upserts, u)
}

func (f *fakeRecorder) Explain(e store.Explain) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.explains = append(f.explains, e)
}

func (f *fakeRecorder) Resolve(r store.Resolve) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolves = append(f.resolves, r)
}

func (f *fakeRecorder) snapshot() ([]store.Upsert, []store.Explain, []store.Resolve) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Upsert(nil), f.upserts...),
		append([]store.Explain(nil), f.explains...),
		append([]store.Resolve(nil), f.resolves...)
}

func TestRecorderReceivesIncidentLifecycle(t *testing.T) {
	be := &fakeBackend{resp: validExplanation()}
	rec := &fakeRecorder{}
	clock := &fakeClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	src := &chanSource{name: "app.log", lines: make(chan string, 16)}

	opts := baseOptions(src, be, &captureSink{})
	opts.Recorder = rec
	opts.Notifications = &captureSink{}
	opts.ThrottleWindow = time.Minute
	opts.ResolveWindow = 10 * time.Second
	opts.resolveTick = 2 * time.Millisecond
	opts.now = clock.Now

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	eng := New(opts)
	go func() { done <- eng.Run(ctx) }()

	send(src, goPanic)

	// The creation is persisted before the explanation, so a UI shows the
	// incident while the model is still working.
	waitFor(t, func() bool {
		upserts, _, _ := rec.snapshot()
		for _, u := range upserts {
			if u.Incident.State == store.StateNew && u.Incident.Count == 1 {
				return true
			}
		}
		return false
	})
	waitFor(t, func() bool {
		_, explains, _ := rec.snapshot()
		return len(explains) == 1
	})

	upserts, explains, _ := rec.snapshot()
	if want := store.ID("app.log", upserts[0].Incident.Fingerprint); upserts[0].Incident.ID != want {
		t.Errorf("persisted id = %q, want %q (source:fingerprint)", upserts[0].Incident.ID, want)
	}
	if explains[0].Explanation.Summary != validExplanation().Summary || explains[0].Explanation.Severity != "high" {
		t.Errorf("persisted explanation = %+v", explains[0].Explanation)
	}

	// Going quiet past the resolve window persists the resolution with the count.
	clock.Advance(11 * time.Second)
	waitFor(t, func() bool {
		_, _, resolves := rec.snapshot()
		return len(resolves) == 1
	})
	_, _, resolves := rec.snapshot()
	if resolves[0].Count != 1 || resolves[0].ID != upserts[0].Incident.ID {
		t.Errorf("persisted resolution = %+v", resolves[0])
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
}
