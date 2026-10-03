package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// baseTime is fixed so stored timestamps are deterministic across a reopen.
var baseTime = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

func openStore(t *testing.T, opts Options) *Store {
	t.Helper()
	if opts.Path == "" {
		opts.Path = filepath.Join(t.TempDir(), "watcher.db")
	}
	s, err := Open(opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// runStore starts the writer loop for a test and returns a stop function.
func runStore(t *testing.T, s *Store) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func sampleIncident() Incident {
	return Incident{
		ID:          ID("web-1", "a1b2c3d4e5f6a7b8"),
		Fingerprint: "a1b2c3d4e5f6a7b8",
		Kind:        "go-panic",
		Source:      "web-1",
		State:       StateNew,
		Count:       1,
		FirstSeen:   baseTime,
		LastSeen:    baseTime,
	}
}

func TestOpenMigratesEmptyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	s := openStore(t, Options{Path: path})

	list, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("fresh database has %d incidents, want 0", len(list))
	}
	if got := s.Stats(); got != (Stats{}) {
		t.Fatalf("fresh stats = %+v, want zero", got)
	}
	if !s.Persistent() {
		t.Fatal("SQLite store should report persistent")
	}

	var version int
	if err := s.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("schema version = %d, want %d", version, schemaVersion)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	s := openStore(t, Options{Path: path})
	runStore(t, s)
	if _, err := s.db.Exec(`UPDATE schema_version SET version = 99`); err != nil {
		t.Fatalf("bump version: %v", err)
	}
	s.Close()

	if _, err := Open(Options{Path: path}); err == nil {
		t.Fatal("Open of a newer schema should fail")
	}
}

func TestRoundTripAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	ctx := context.Background()

	s := openStore(t, Options{Path: path})
	runStore(t, s)

	inc := sampleIncident()
	s.Observe(Upsert{Incident: inc, OccurredAt: baseTime})
	waitFor(t, "incident stored", func() bool {
		l, _ := s.List(ctx)
		return len(l) == 1
	})

	expl := Explanation{
		Model:        "test:1b",
		Summary:      "nil dereference in the request handler",
		LikelyCause:  "the handler dereferenced a nil request body",
		Evidence:     []string{"TypeError: cannot read properties of null", "at handle (/app/server.js:42)"},
		SuggestedFix: "guard the request body before dereferencing",
		Confidence:   0.72,
		Severity:     "high",
	}
	s.Explain(Explain{ID: inc.ID, Fingerprint: inc.Fingerprint, At: baseTime.Add(time.Second), Explanation: expl})
	waitFor(t, "explanation stored", func() bool {
		d, err := s.Get(ctx, inc.ID)
		return err == nil && d.Latest != nil
	})
	s.Resolve(Resolve{ID: inc.ID, ResolvedAt: baseTime.Add(time.Minute), Count: 1})
	waitFor(t, "incident resolved", func() bool {
		d, err := s.Get(ctx, inc.ID)
		return err == nil && d.Incident.State == StateResolved
	})
	s.Close()

	// Reopen: history must be present, with counters, state, and explanation.
	reopened := openStore(t, Options{Path: path})
	detail, err := reopened.Get(ctx, inc.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if detail.Incident.State != StateResolved {
		t.Errorf("state = %q, want resolved", detail.Incident.State)
	}
	if detail.Incident.Count != 1 || detail.Incident.Source != "web-1" || detail.Incident.Kind != "go-panic" {
		t.Errorf("incident identity lost: %+v", detail.Incident)
	}
	if !detail.Incident.FirstSeen.Equal(baseTime) || !detail.Incident.LastSeen.Equal(baseTime) {
		t.Errorf("first/last seen = %v/%v, want %v", detail.Incident.FirstSeen, detail.Incident.LastSeen, baseTime)
	}
	if detail.Incident.Severity != "high" {
		t.Errorf("severity = %q, want high", detail.Incident.Severity)
	}
	if detail.Incident.Summary != expl.Summary {
		t.Errorf("summary = %q, want %q", detail.Incident.Summary, expl.Summary)
	}
	if detail.Latest == nil {
		t.Fatal("latest explanation lost across reopen")
	}
	if len(detail.Latest.Evidence) != 2 || detail.Latest.Evidence[0] != expl.Evidence[0] {
		t.Errorf("evidence = %v, want %v", detail.Latest.Evidence, expl.Evidence)
	}
	if detail.Latest.Confidence != 0.72 {
		t.Errorf("confidence = %v, want 0.72", detail.Latest.Confidence)
	}
	if len(detail.Occurrences) != 1 || !detail.Occurrences[0].Equal(baseTime) {
		t.Errorf("occurrences = %v, want [%v]", detail.Occurrences, baseTime)
	}
}

func TestGetUnknownIsNotFound(t *testing.T) {
	s := openStore(t, Options{})
	if _, err := s.Get(context.Background(), "nope:1234"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get unknown err = %v, want ErrNotFound", err)
	}
}

func TestSeparateIncidentsPerLabel(t *testing.T) {
	s := openStore(t, Options{})
	runStore(t, s)
	ctx := context.Background()

	for _, label := range []string{"web-1", "worker-2"} {
		inc := sampleIncident()
		inc.ID = ID(label, inc.Fingerprint)
		inc.Source = label
		s.Observe(Upsert{Incident: inc, OccurredAt: baseTime})
	}
	waitFor(t, "both incidents stored", func() bool {
		l, _ := s.List(ctx)
		return len(l) == 2
	})
	// The same fingerprint on two labels is two rows, keyed by distinct ids.
	if got := s.Stats(); got.Total != 2 {
		t.Fatalf("stats = %+v, want 2 total", got)
	}
}

func TestOccurrenceRetentionCap(t *testing.T) {
	s := openStore(t, Options{OccurrenceCap: 3})
	runStore(t, s)
	ctx := context.Background()

	inc := sampleIncident()
	for i := 0; i < 6; i++ {
		at := baseTime.Add(time.Duration(i) * time.Second)
		inc.Count = i + 1
		inc.LastSeen = at
		inc.State = StateOngoing
		s.Observe(Upsert{Incident: inc, OccurredAt: at})
	}
	waitFor(t, "all six occurrences stored", func() bool {
		var n int
		s.db.QueryRow(`SELECT COUNT(*) FROM occurrences WHERE incident_id = ?`, inc.ID).Scan(&n)
		return n == 6
	})

	s.trim(ctx)

	var kept int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM occurrences WHERE incident_id = ?`, inc.ID).Scan(&kept); err != nil {
		t.Fatalf("count occurrences: %v", err)
	}
	if kept != 3 {
		t.Fatalf("occurrences kept = %d, want 3", kept)
	}
	detail, err := s.Get(ctx, inc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Newest three survive, most recent first.
	if len(detail.Occurrences) != 3 {
		t.Fatalf("detail occurrences = %d, want 3", len(detail.Occurrences))
	}
	if want := baseTime.Add(5 * time.Second); !detail.Occurrences[0].Equal(want) {
		t.Fatalf("newest occurrence = %v, want %v", detail.Occurrences[0], want)
	}
}

func TestAgeRetentionRemovesStaleIncidents(t *testing.T) {
	s := openStore(t, Options{Retention: time.Hour})
	runStore(t, s)
	ctx := context.Background()

	old := sampleIncident()
	old.LastSeen = time.Now().Add(-2 * time.Hour)
	s.Observe(Upsert{Incident: old, OccurredAt: old.LastSeen})
	s.Explain(Explain{ID: old.ID, Fingerprint: old.Fingerprint, At: old.LastSeen, Explanation: Explanation{Model: "m", Summary: "old"}})

	fresh := sampleIncident()
	fresh.ID = ID("web-2", fresh.Fingerprint)
	fresh.Source = "web-2"
	fresh.LastSeen = time.Now()
	s.Observe(Upsert{Incident: fresh, OccurredAt: fresh.LastSeen})

	waitFor(t, "both incidents stored", func() bool {
		l, _ := s.List(ctx)
		return len(l) == 2
	})

	s.trim(ctx)

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != fresh.ID {
		t.Fatalf("after age trim list = %+v, want only %s", list, fresh.ID)
	}
	// The stale incident's children are removed with it, not orphaned.
	var orphans int
	s.db.QueryRow(`SELECT COUNT(*) FROM explanations WHERE incident_id = ?`, old.ID).Scan(&orphans)
	if orphans != 0 {
		t.Fatalf("orphan explanations = %d, want 0", orphans)
	}
}

func TestEventsPublishedPerMutation(t *testing.T) {
	s := openStore(t, Options{})
	runStore(t, s)

	ch, cancel := s.Subscribe()
	defer cancel()

	inc := sampleIncident()
	s.Observe(Upsert{Incident: inc, OccurredAt: baseTime})
	inc.State = StateOngoing
	inc.Count = 2
	s.Observe(Upsert{Incident: inc, OccurredAt: baseTime.Add(time.Second)})
	s.Explain(Explain{ID: inc.ID, Fingerprint: inc.Fingerprint, At: baseTime.Add(2 * time.Second), Explanation: Explanation{Model: "m", Summary: "s", Severity: "high"}})
	s.Resolve(Resolve{ID: inc.ID, ResolvedAt: baseTime.Add(time.Minute), Count: 2})

	want := []EventType{EventCreated, EventCounted, EventExplained, EventStateChanged}
	for i, wantType := range want {
		select {
		case ev := <-ch:
			if ev.Type != wantType {
				t.Fatalf("event %d type = %q, want %q", i, ev.Type, wantType)
			}
			if ev.ID != inc.ID {
				t.Fatalf("event %d id = %q, want %q", i, ev.ID, inc.ID)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for event %d (%s)", i, wantType)
		}
	}
}

func TestReadConnectionRejectsWrites(t *testing.T) {
	s := openStore(t, Options{})
	if _, err := s.ro.Exec(`INSERT INTO incidents (id, fingerprint, kind, source, state, count, first_seen, last_seen) VALUES ('x','x','k','s','new',1,'t','t')`); err == nil {
		t.Fatal("read-only connection should reject a write")
	}
}

func TestMemoryBackendServesButDoesNotPersist(t *testing.T) {
	m := NewMemory(2)
	if m.Persistent() {
		t.Fatal("memory backend should not be persistent")
	}
	ctx := context.Background()

	inc := sampleIncident()
	m.Observe(Upsert{Incident: inc, OccurredAt: baseTime})
	m.Explain(Explain{ID: inc.ID, Fingerprint: inc.Fingerprint, At: baseTime, Explanation: Explanation{Model: "m", Summary: "boom", Severity: "critical"}})
	m.Resolve(Resolve{ID: inc.ID, ResolvedAt: baseTime.Add(time.Minute), Count: 1})

	detail, err := m.Get(ctx, inc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.Incident.State != StateResolved || detail.Incident.Severity != "critical" {
		t.Fatalf("detail = %+v, want resolved/critical", detail.Incident)
	}
	if detail.Latest == nil || detail.Latest.Summary != "boom" {
		t.Fatalf("latest = %+v, want summary boom", detail.Latest)
	}
	if got := m.Stats(); got != (Stats{Total: 1, Resolved: 1}) {
		t.Fatalf("stats = %+v, want 1 total / 1 resolved", got)
	}
	if _, err := m.Get(ctx, "missing:0"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err = %v, want ErrNotFound", err)
	}
}

func TestMemoryOccurrenceCap(t *testing.T) {
	m := NewMemory(2)
	inc := sampleIncident()
	for i := 0; i < 5; i++ {
		m.Observe(Upsert{Incident: inc, OccurredAt: baseTime.Add(time.Duration(i) * time.Second)})
	}
	detail, err := m.Get(context.Background(), inc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(detail.Occurrences) != 2 {
		t.Fatalf("occurrences = %d, want 2 (capped)", len(detail.Occurrences))
	}
}

// ensure the driver is registered and usable directly, catching a broken import
// even if no other test opens a file.
func TestDriverRegistered(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping sqlite: %v", err)
	}
}
