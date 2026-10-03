package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaheeranser/watcher/internal/store"
)

type fakeReader struct {
	list       []store.Incident
	details    map[string]store.Detail
	stats      store.Stats
	persistent bool
}

func (f fakeReader) List(context.Context) ([]store.Incident, error) { return f.list, nil }

func (f fakeReader) Get(_ context.Context, id string) (store.Detail, error) {
	d, ok := f.details[id]
	if !ok {
		return store.Detail{}, store.ErrNotFound
	}
	return d, nil
}

func (f fakeReader) Stats() store.Stats { return f.stats }
func (f fakeReader) Persistent() bool   { return f.persistent }

// startServer binds a real Unix socket in a short temp directory (the sun_path
// limit is 108 bytes) and serves the API on it.
func startServer(t *testing.T, reader store.Reader, events EventSource) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "wapi")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "w.sock")

	s := New(Options{Socket: socket, Reader: reader, Events: events})
	l, err := s.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Serve(ctx, l) }()
	t.Cleanup(func() { cancel(); <-done })

	return NewClient(socket)
}

func sampleRow() store.Incident {
	return store.Incident{
		ID:             store.ID("web-1", "a1b2c3d4e5f6a7b8"),
		Fingerprint:    "a1b2c3d4e5f6a7b8",
		Kind:           "go-panic",
		Source:         "web-1",
		Severity:       "high",
		State:          store.StateOngoing,
		Count:          12,
		FirstSeen:      time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
		LastSeen:       time.Date(2026, 1, 2, 15, 6, 11, 0, time.UTC),
		Summary:        "panic: send on closed channel",
		HasExplanation: true,
	}
}

func TestHealth(t *testing.T) {
	client := startServer(t, fakeReader{
		stats:      store.Stats{Total: 9, Active: 7, Resolved: 2},
		persistent: true,
	}, nil)

	h, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.Status != "ok" || h.APIVersion != Version || h.Version == "" {
		t.Fatalf("health = %+v", h)
	}
	if h.IncidentsTracked != 9 || h.Active != 7 || h.Resolved != 2 || !h.Persistent {
		t.Fatalf("health counters = %+v", h)
	}
}

func TestListShape(t *testing.T) {
	client := startServer(t, fakeReader{list: []store.Incident{sampleRow()}}, nil)

	rows, err := client.Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.ID != store.ID("web-1", "a1b2c3d4e5f6a7b8") || r.Kind != "go-panic" || r.Source != "web-1" {
		t.Fatalf("identity = %+v", r)
	}
	if r.Count != 12 || r.State != "ongoing" || r.Severity != "high" {
		t.Fatalf("counters = %+v", r)
	}
	if r.LastSeen != "2026-01-02T15:06:11Z" {
		t.Fatalf("last_seen = %q", r.LastSeen)
	}
	if r.ExplanationPending || r.ExplanationUnavailable {
		t.Fatalf("explained row should be neither pending nor unavailable: %+v", r)
	}
}

func TestListEmptyIsArray(t *testing.T) {
	s := New(Options{Reader: fakeReader{}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, Prefix+"/incidents", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("empty list body = %q, want []", got)
	}
}

func TestPendingAndUnavailableMarkers(t *testing.T) {
	pending := sampleRow()
	pending.ID = store.ID("a", "1")
	pending.HasExplanation = false
	pending.Summary = ""

	failed := sampleRow()
	failed.ID = store.ID("b", "2")
	failed.HasExplanation = true
	failed.ExplanationError = "ollama unreachable"
	failed.Summary = ""

	client := startServer(t, fakeReader{list: []store.Incident{pending, failed}}, nil)
	rows, err := client.Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if !rows[0].ExplanationPending || rows[0].ExplanationUnavailable {
		t.Fatalf("pending row = %+v", rows[0])
	}
	if rows[1].ExplanationPending || !rows[1].ExplanationUnavailable {
		t.Fatalf("failed row = %+v", rows[1])
	}
}

func TestDetailShapeAndNotFound(t *testing.T) {
	inc := sampleRow()
	detail := store.Detail{
		Incident: inc,
		Latest: &store.Explanation{
			Model:        "test:1b",
			Summary:      "send on closed channel",
			LikelyCause:  "the hub closed before Broadcast ran",
			Evidence:     []string{"panic: send on closed channel", "main.(*Hub).Broadcast(...)"},
			SuggestedFix: "select on a done channel before sending",
			Confidence:   0.72,
			Severity:     "high",
		},
		Occurrences: []time.Time{
			time.Date(2026, 1, 2, 15, 6, 11, 0, time.UTC),
			time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
		},
	}
	client := startServer(t, fakeReader{details: map[string]store.Detail{inc.ID: detail}}, nil)
	ctx := context.Background()

	got, err := client.Incident(ctx, inc.ID)
	if err != nil {
		t.Fatalf("Incident: %v", err)
	}
	if got.LikelyCause != "the hub closed before Broadcast ran" {
		t.Fatalf("likely_cause = %q", got.LikelyCause)
	}
	if len(got.Evidence) != 2 || got.SuggestedFix == "" || got.Confidence != 0.72 {
		t.Fatalf("detail = %+v", got)
	}
	if len(got.Occurrences) != 2 || got.Occurrences[0] != "2026-01-02T15:06:11Z" {
		t.Fatalf("occurrences = %v", got.Occurrences)
	}

	if _, err := client.Incident(ctx, store.ID("nope", "0000")); err == nil {
		t.Fatal("unknown id should return an error")
	}
}

func TestIncidentIDRoundTripsSlashAndSpace(t *testing.T) {
	for _, source := range []string{"var/log/app.log", "web 1", "backend/api"} {
		inc := sampleRow()
		inc.ID = store.ID(source, inc.Fingerprint)
		inc.Source = source
		client := startServer(t, fakeReader{
			details: map[string]store.Detail{inc.ID: {Incident: inc}},
		}, nil)

		got, err := client.Incident(context.Background(), inc.ID)
		if err != nil {
			t.Fatalf("source %q: Incident: %v", source, err)
		}
		if got.Source != source {
			t.Fatalf("source %q round-tripped as %q", source, got.Source)
		}
	}
}

func TestSSEEmitsPerMutation(t *testing.T) {
	mem := store.NewMemory(0)
	client := startServer(t, fakeReader{}, mem)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := client.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	defer stream.Close()

	// Publish until the handler has subscribed, then read the first event.
	inc := sampleRow()
	inc.State = store.StateNew
	inc.Count = 1
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			mem.Observe(store.Upsert{Incident: inc, OccurredAt: time.Now()})
			time.Sleep(10 * time.Millisecond)
		}
	}()
	defer close(stop)

	done := make(chan struct{})
	go func() {
		defer close(done)
		ev, err := stream.Next()
		if err != nil {
			return
		}
		if ev.Type != store.EventCreated || ev.Fingerprint != "a1b2c3d4e5f6a7b8" {
			t.Errorf("event = %+v", ev)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an SSE event")
	}
}

func TestStalledSubscriberDoesNotBlock(t *testing.T) {
	mem := store.NewMemory(0)
	_, cancel := mem.Subscribe() // never read
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 512; i++ {
			mem.Observe(store.Upsert{Incident: sampleRow(), OccurredAt: time.Now()})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publishing blocked on a stalled subscriber")
	}
}

func TestListenRejectsLiveSocketAndSetsMode(t *testing.T) {
	dir, err := os.MkdirTemp("", "wapi")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "w.sock")

	s := New(Options{Socket: socket, Reader: fakeReader{}})
	l, err := s.Listen()
	if err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	defer l.Close()

	info, err := os.Stat(socket)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o, want 600", info.Mode().Perm())
	}

	// A second bind must fail loudly rather than steal or shadow the path.
	if _, err := New(Options{Socket: socket}).Listen(); err == nil {
		t.Fatal("second Listen on a live socket should fail")
	}
}

func TestSocketReplacesStaleFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "wapi")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "w.sock")
	if err := os.WriteFile(socket, []byte("stale"), 0o600); err != nil {
		t.Fatalf("seed stale socket: %v", err)
	}
	l, err := New(Options{Socket: socket}).Listen()
	if err != nil {
		t.Fatalf("Listen over a stale file: %v", err)
	}
	l.Close()
}

func TestJSONErrorBody(t *testing.T) {
	client := startServer(t, fakeReader{}, nil)
	ctx := context.Background()

	if _, err := client.Incident(ctx, "missing:1"); err == nil {
		t.Fatal("want an error")
	} else if !strings.Contains(err.Error(), "unknown incident") {
		t.Fatalf("error = %q, want it to carry the server reason", err)
	}
}

// compile-time checks: the wire types marshal with the expected keys.
func TestWireKeys(t *testing.T) {
	b, err := json.Marshal(IncidentRow{ID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	for _, key := range []string{"id", "fingerprint", "kind", "source", "severity", "state", "count", "first_seen", "last_seen", "summary", "explanation_pending", "explanation_unavailable"} {
		if _, ok := m[key]; !ok {
			t.Errorf("list row is missing key %q", key)
		}
	}
}
