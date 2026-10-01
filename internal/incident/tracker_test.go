package incident

import (
	"testing"
	"time"

	"github.com/shaheeranser/watcher/internal/backend"
)

func TestObserveCountsAndGatesExplanations(t *testing.T) {
	tr := NewTracker(15 * time.Minute)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	first, due := tr.Observe("app.log", "fp1", "go-panic", base)
	if !due {
		t.Fatal("the first sighting must be due for an explanation")
	}
	if first.Count != 1 || !first.FirstSeen.Equal(base) || !first.LastSeen.Equal(base) {
		t.Errorf("unexpected first observation: %+v", first)
	}

	second, due := tr.Observe("app.log", "fp1", "go-panic", base.Add(time.Minute))
	if due {
		t.Fatal("a repeat inside the window must not be due")
	}
	if second.Count != 2 {
		t.Errorf("Count = %d, want 2", second.Count)
	}
	if !second.FirstSeen.Equal(base) || !second.LastSeen.Equal(base.Add(time.Minute)) {
		t.Errorf("times not tracked correctly: %+v", second)
	}
}

func TestWindowExpiryAllowsFreshExplanation(t *testing.T) {
	tr := NewTracker(15 * time.Minute)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	tr.Observe("app.log", "fp1", "go-panic", base)
	if _, due := tr.Observe("app.log", "fp1", "go-panic", base.Add(14*time.Minute)); due {
		t.Fatal("inside the window it must not be due")
	}
	if _, due := tr.Observe("app.log", "fp1", "go-panic", base.Add(16*time.Minute)); !due {
		t.Fatal("after the window it must be due again")
	}
}

func TestZeroWindowExplainsEveryOccurrence(t *testing.T) {
	tr := NewTracker(0)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if _, due := tr.Observe("app.log", "fp1", "go-panic", base); !due {
			t.Fatalf("zero window must always be due (iteration %d)", i)
		}
	}
}

func TestRecordedExplanationIsReused(t *testing.T) {
	tr := NewTracker(15 * time.Minute)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tr.Observe("app.log", "fp1", "go-panic", base)

	expl := &backend.Explanation{Summary: "nil deref", Severity: "high", Confidence: 0.7}
	tr.RecordExplanation("app.log", "fp1", expl, "test-model", "")

	repeat, due := tr.Observe("app.log", "fp1", "go-panic", base.Add(time.Minute))
	if due {
		t.Fatal("repeat must not be due")
	}
	if repeat.Explanation == nil || repeat.Explanation.Summary != "nil deref" {
		t.Errorf("cached explanation missing: %+v", repeat.Explanation)
	}
	if repeat.Model != "test-model" {
		t.Errorf("Model = %q, want test-model", repeat.Model)
	}
}

func TestFingerprintsAreIndependent(t *testing.T) {
	tr := NewTracker(15 * time.Minute)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	tr.Observe("app.log", "fp1", "go-panic", base)
	second, due := tr.Observe("app.log", "fp2", "python-traceback", base)
	if !due {
		t.Fatal("a new fingerprint must be due regardless of others")
	}
	if second.Count != 1 || second.Kind != "python-traceback" {
		t.Errorf("unexpected second incident: %+v", second)
	}
}

func TestSameFingerprintOnTwoLabelsIsTwoIncidents(t *testing.T) {
	tr := NewTracker(15 * time.Minute)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	backendInc, due := tr.Observe("backend", "fpsame", "go-panic", base)
	if !due || backendInc.Source != "backend" {
		t.Fatalf("unexpected backend incident: %+v due=%v", backendInc, due)
	}
	frontendInc, due := tr.Observe("frontend", "fpsame", "go-panic", base)
	if !due {
		t.Fatal("the same fingerprint on a different label is a distinct, fresh incident")
	}
	if frontendInc.Count != 1 || frontendInc.Source != "frontend" {
		t.Errorf("unexpected frontend incident: %+v", frontendInc)
	}

	again, _ := tr.Observe("backend", "fpsame", "go-panic", base.Add(time.Second))
	if again.Count != 2 {
		t.Errorf("backend count = %d, want 2", again.Count)
	}
}
