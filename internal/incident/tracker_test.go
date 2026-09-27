package incident

import (
	"testing"
	"time"

	"github.com/shaheeranser/watcher/internal/backend"
)

func TestObserveCountsAndGatesExplanations(t *testing.T) {
	tr := NewTracker(15 * time.Minute)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	first, due := tr.Observe("fp1", "go-panic", "app.log", base)
	if !due {
		t.Fatal("the first sighting must be due for an explanation")
	}
	if first.Count != 1 || !first.FirstSeen.Equal(base) || !first.LastSeen.Equal(base) {
		t.Errorf("unexpected first observation: %+v", first)
	}

	second, due := tr.Observe("fp1", "go-panic", "app.log", base.Add(time.Minute))
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

	tr.Observe("fp1", "go-panic", "app.log", base)
	if _, due := tr.Observe("fp1", "go-panic", "app.log", base.Add(14*time.Minute)); due {
		t.Fatal("inside the window it must not be due")
	}
	if _, due := tr.Observe("fp1", "go-panic", "app.log", base.Add(16*time.Minute)); !due {
		t.Fatal("after the window it must be due again")
	}
}

func TestZeroWindowExplainsEveryOccurrence(t *testing.T) {
	tr := NewTracker(0)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if _, due := tr.Observe("fp1", "go-panic", "app.log", base); !due {
			t.Fatalf("zero window must always be due (iteration %d)", i)
		}
	}
}

func TestRecordedExplanationIsReused(t *testing.T) {
	tr := NewTracker(15 * time.Minute)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tr.Observe("fp1", "go-panic", "app.log", base)

	expl := &backend.Explanation{Summary: "nil deref", Severity: "high", Confidence: 0.7}
	tr.RecordExplanation("fp1", expl, "test-model", "")

	repeat, due := tr.Observe("fp1", "go-panic", "app.log", base.Add(time.Minute))
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

	tr.Observe("fp1", "go-panic", "app.log", base)
	second, due := tr.Observe("fp2", "python-traceback", "app.log", base)
	if !due {
		t.Fatal("a new fingerprint must be due regardless of others")
	}
	if second.Count != 1 || second.Kind != "python-traceback" {
		t.Errorf("unexpected second incident: %+v", second)
	}
}
