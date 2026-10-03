package incident

import (
	"sync"
	"time"

	"github.com/shaheeranser/watcher/internal/backend"
)

// key identifies an incident. The label is part of the key, not the fingerprint
// hash, so the same crash text on two sources stays two incidents while
// milestone 01's fingerprints remain unchanged.
type key struct {
	label       string
	fingerprint string
}

// Tracker is the process-local incident table. It is safe for concurrent use
// because a backend pool may observe several fingerprints at once.
type Tracker struct {
	mu        sync.Mutex
	window    time.Duration
	incidents map[key]*Incident
}

// NewTracker builds a tracker whose explanation window is the minimum interval
// between model calls for one incident. A zero window explains every
// occurrence.
func NewTracker(window time.Duration) *Tracker {
	return &Tracker{window: window, incidents: make(map[key]*Incident)}
}

// Observe records one occurrence of a (label, fingerprint) crash and reports
// whether a fresh explanation is due. When it is, the window is started right
// away so that a second observer looking at the same incident does not also
// decide to call the model (CORE-FP-6).
func (t *Tracker) Observe(label, fingerprint, kind string, now time.Time) (Incident, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	k := key{label: label, fingerprint: fingerprint}
	inc, ok := t.incidents[k]
	if !ok {
		inc = &Incident{Fingerprint: fingerprint, Kind: kind, Source: label}
		t.incidents[k] = inc
	}
	// A reset incident has no count, so this occurrence opens a new cycle.
	if inc.Count == 0 {
		inc.FirstSeen = now
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
// not, so later occurrences can reuse it, and returns the updated incident.
func (t *Tracker) RecordExplanation(label, fingerprint string, expl *backend.Explanation, model, explainErr string) Incident {
	t.mu.Lock()
	defer t.mu.Unlock()

	inc, ok := t.incidents[key{label: label, fingerprint: fingerprint}]
	if !ok {
		return Incident{}
	}
	inc.Explanation = expl
	inc.Model = model
	inc.ExplainErr = explainErr
	return *inc
}

// Reset forgets an incident's accumulated count, first-seen time, explanation,
// and explanation window so that a fingerprint which recurs after being
// resolved starts a genuinely fresh cycle (PROD-STM-4, OD-02-5). It is a no-op
// for an incident the tracker has never seen.
func (t *Tracker) Reset(label, fingerprint string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	inc, ok := t.incidents[key{label: label, fingerprint: fingerprint}]
	if !ok {
		return
	}
	inc.Count = 0
	inc.FirstSeen = time.Time{}
	inc.LastExplainedAt = time.Time{}
	inc.Explanation = nil
	inc.ExplainErr = ""
}

// Lookup returns a snapshot of one incident, so a notification assembled
// outside the observe path (a resolved tick) sees the same counters as the
// stream did.
func (t *Tracker) Lookup(label, fingerprint string) (Incident, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	inc, ok := t.incidents[key{label: label, fingerprint: fingerprint}]
	if !ok {
		return Incident{}, false
	}
	return *inc, true
}

// Len reports how many distinct incidents are tracked, for liveness reporting.
func (t *Tracker) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.incidents)
}
