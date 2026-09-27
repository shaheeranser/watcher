package incident

import (
	"sync"
	"time"

	"github.com/shaheeranser/watcher/internal/backend"
)

// Tracker is the process-local incident table. It is safe for concurrent use
// because a backend pool may observe several fingerprints at once.
type Tracker struct {
	mu        sync.Mutex
	window    time.Duration
	incidents map[string]*Incident
}

// NewTracker builds a tracker whose explanation window is the minimum interval
// between model calls for one fingerprint. A zero window explains every
// occurrence.
func NewTracker(window time.Duration) *Tracker {
	return &Tracker{window: window, incidents: make(map[string]*Incident)}
}

// Observe records one occurrence and reports whether a fresh explanation is
// due. When it is, the window is started right away so that a second observer
// looking at the same fingerprint does not also decide to call the model
// (CORE-FP-6).
func (t *Tracker) Observe(fingerprint, kind, source string, now time.Time) (Incident, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	inc, ok := t.incidents[fingerprint]
	if !ok {
		inc = &Incident{Fingerprint: fingerprint, Kind: kind, Source: source, FirstSeen: now}
		t.incidents[fingerprint] = inc
	}
	inc.Count++
	inc.LastSeen = now

	due := inc.LastExplainedAt.IsZero() || now.Sub(inc.LastExplainedAt) >= t.window
	if due {
		inc.LastExplainedAt = now
	}
	return *inc, due
}

// RecordExplanation stores the outcome of an explanation attempt, successful or
// not, so later occurrences can reuse it.
func (t *Tracker) RecordExplanation(fingerprint string, expl *backend.Explanation, model, explainErr string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	inc, ok := t.incidents[fingerprint]
	if !ok {
		return
	}
	inc.Explanation = expl
	inc.Model = model
	inc.ExplainErr = explainErr
}
