package store

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Memory is the in-memory Backend used when the database cannot be opened. It
// keeps the daemon detecting and notifying and the API serving current state, so
// a persistence failure degrades history rather than the product (DASH-26). It
// holds nothing from before the process started, and a restart keeps nothing.
type Memory struct {
	mu            sync.Mutex
	incidents     map[string]Incident
	explanations  map[string][]Explanation
	occurrences   map[string][]time.Time
	events        *broker
	occurrenceCap int
}

// NewMemory builds an in-memory backend with the same occurrence cap the SQLite
// backend uses, so both bound their per-incident history the same way.
func NewMemory(occurrenceCap int) *Memory {
	return &Memory{
		incidents:     make(map[string]Incident),
		explanations:  make(map[string][]Explanation),
		occurrences:   make(map[string][]time.Time),
		events:        newBroker(),
		occurrenceCap: occurrenceCap,
	}
}

// Run blocks until ctx is cancelled, matching the SQLite backend's lifecycle so
// the daemon can start either one the same way.
func (m *Memory) Run(ctx context.Context) { <-ctx.Done() }

// Close releases subscribers.
func (m *Memory) Close() error {
	m.events.close()
	return nil
}

// Persistent reports that this backend does not write to disk.
func (m *Memory) Persistent() bool { return false }

// Dropped is always zero: in-memory writes never queue.
func (m *Memory) Dropped() int64 { return 0 }

// Subscribe returns an event stream and its cancel function.
func (m *Memory) Subscribe() (<-chan Event, func()) { return m.events.subscribe() }

// Observe records one occurrence, replacing the incident's current counters with
// the tracker's authoritative snapshot.
func (m *Memory) Observe(u Upsert) {
	m.mu.Lock()
	defer m.mu.Unlock()

	inc := u.Incident
	m.incidents[inc.ID] = inc
	m.occurrences[inc.ID] = appendCapped(m.occurrences[inc.ID], u.OccurredAt, m.occurrenceCap)

	kind := EventCounted
	if inc.State == StateNew {
		kind = EventCreated
	}
	m.events.publish(Event{
		Type: kind, ID: inc.ID, Fingerprint: inc.Fingerprint, Source: inc.Source,
		Count: inc.Count, State: string(inc.State), Fields: []string{"count", "last_seen"},
	})
}

// Explain records one explanation attempt and folds its summary and status back
// onto the incident row.
func (m *Memory) Explain(e Explain) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.explanations[e.ID] = append(m.explanations[e.ID], e.Explanation)
	inc, ok := m.incidents[e.ID]
	if !ok {
		return
	}
	inc.HasExplanation = true
	inc.Summary = e.Explanation.Summary
	inc.ExplanationError = e.Explanation.Error
	if e.Explanation.Severity != "" && e.Explanation.Error == "" {
		inc.Severity = e.Explanation.Severity
	}
	m.incidents[e.ID] = inc
	m.events.publish(Event{
		Type: EventExplained, ID: e.ID, Fingerprint: e.Fingerprint,
		Fields: []string{"summary", "likely_cause", "evidence", "suggested_fix", "confidence", "severity"},
	})
}

// Resolve marks an incident resolved.
func (m *Memory) Resolve(r Resolve) {
	m.mu.Lock()
	defer m.mu.Unlock()

	inc, ok := m.incidents[r.ID]
	if !ok {
		return
	}
	inc.State = StateResolved
	inc.ResolvedAt = r.ResolvedAt
	inc.Count = r.Count
	m.incidents[r.ID] = inc
	m.events.publish(Event{
		Type: EventStateChanged, ID: r.ID, Count: r.Count, State: string(StateResolved),
		Fields: []string{"state", "resolved_at"},
	})
}

// List returns every incident, most recently active first.
func (m *Memory) List(_ context.Context) ([]Incident, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Incident, 0, len(m.incidents))
	for _, inc := range m.incidents {
		out = append(out, inc)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].ID < out[j].ID
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out, nil
}

// Get returns one incident's detail, or ErrNotFound.
func (m *Memory) Get(_ context.Context, id string) (Detail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	inc, ok := m.incidents[id]
	if !ok {
		return Detail{}, ErrNotFound
	}
	detail := Detail{Incident: inc}
	if ex := m.explanations[id]; len(ex) > 0 {
		latest := ex[len(ex)-1]
		detail.Latest = &latest
	}
	occurrences := append([]time.Time(nil), m.occurrences[id]...)
	sort.Slice(occurrences, func(i, j int) bool { return occurrences[i].After(occurrences[j]) })
	if len(occurrences) > occurrenceWindow {
		occurrences = occurrences[:occurrenceWindow]
	}
	detail.Occurrences = occurrences
	return detail, nil
}

// Stats counts incidents for the health endpoint.
func (m *Memory) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()

	var st Stats
	for _, inc := range m.incidents {
		st.Total++
		if inc.State == StateResolved {
			st.Resolved++
		} else {
			st.Active++
		}
	}
	return st
}

// appendCapped appends t and keeps at most cap entries, newest retained.
func appendCapped(seen []time.Time, t time.Time, cap int) []time.Time {
	seen = append(seen, t)
	if cap > 0 && len(seen) > cap {
		seen = seen[len(seen)-cap:]
	}
	return seen
}
