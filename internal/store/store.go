// Package store persists incident history and serves it to the read API. It is
// a projection of the engine's incident tracker, not its source of truth: the
// tracker's in-memory map still decides behaviour, and storage failures never
// stop detection or notification (DASH-26). Writes go through a single
// goroutine behind a bounded queue so the pipeline never blocks on disk
// (DASH-NFR-2), and each committed mutation is published to subscribers so the
// API can push live updates without polling (DASH-19).
package store

import (
	"context"
	"errors"
	"time"
)

// State is the incident lifecycle state the dashboard shows.
type State string

const (
	StateNew      State = "new"
	StateOngoing  State = "ongoing"
	StateResolved State = "resolved"
)

// Incident is one row of the incident list.
type Incident struct {
	ID          string
	Fingerprint string
	Kind        string
	Source      string
	Severity    string
	State       State
	Count       int
	FirstSeen   time.Time
	LastSeen    time.Time
	ResolvedAt  time.Time

	// Summary is the latest explanation's summary, and the two flags describe
	// that attempt: HasExplanation is false while the model is still working, and
	// ExplanationError is set when the attempt failed. Together they let a list
	// row show "analysing" or "unavailable" instead of a blank (DASH-18).
	Summary          string
	HasExplanation   bool
	ExplanationError string
}

// Explanation is one explanation attempt, kept as history rather than
// overwritten so a re-explanation after the window is preserved.
type Explanation struct {
	Model        string
	Summary      string
	LikelyCause  string
	Evidence     []string
	SuggestedFix string
	Confidence   float64
	Severity     string
	Pending      bool
	Error        string
}

// Detail is one incident with its latest explanation and recent occurrences.
type Detail struct {
	Incident
	Latest      *Explanation
	Occurrences []time.Time
}

// Upsert records one occurrence: the incident's current counters and state, and
// when it was seen. It is an upsert rather than a delta so a dropped write heals
// on the next occurrence.
type Upsert struct {
	Incident   Incident
	OccurredAt time.Time
}

// Explain records one explanation attempt against an existing incident.
type Explain struct {
	ID          string
	Fingerprint string
	At          time.Time
	Explanation Explanation
}

// Resolve marks an incident resolved.
type Resolve struct {
	ID         string
	ResolvedAt time.Time
	Count      int
}

// Recorder is the write side the engine calls. Implementations must not block
// the caller on disk.
type Recorder interface {
	Observe(Upsert)
	Explain(Explain)
	Resolve(Resolve)
}

// Reader is the read side the API serves.
type Reader interface {
	List(ctx context.Context) ([]Incident, error)
	Get(ctx context.Context, id string) (Detail, error)
	Stats() Stats
	Persistent() bool
}

// Backend is a complete storage backend: the engine's write side, the API's
// read side, the event stream, and its own lifecycle.
type Backend interface {
	Recorder
	Reader
	Run(ctx context.Context)
	Close() error
	Subscribe() (<-chan Event, func())
	Dropped() int64
}

// Stats summarizes stored incidents for the health endpoint.
type Stats struct {
	Total    int
	Active   int
	Resolved int
}

// ErrNotFound is returned by Get for an unknown incident id.
var ErrNotFound = errors.New("incident not found")

// ID is the stable identifier of one incident: the source label joined with the
// fingerprint. The label is part of the identity because the same crash text on
// two sources is two incidents (01b §3.2), so the fingerprint alone cannot key a
// row. Both halves are stable across restarts, which is what lets the UI reselect
// the same incident after a reconnect or a daemon restart (DASH-9).
func ID(source, fingerprint string) string {
	return source + ":" + fingerprint
}
