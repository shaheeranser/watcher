// Package api serves the daemon's incident history over a local read-only HTTP
// endpoint and provides the client the `attach` TUI uses. It never writes: the
// daemon owns state, the API projects it, and attach only reads (DASH-10). The
// surface is versioned under /api/v1 so the TUI and the daemon can be upgraded
// independently (DASH-NFR-5).
package api

import (
	"time"

	"github.com/shaheeranser/watcher/internal/store"
)

// Version is the API version segment every route carries.
const Version = "v1"

// Prefix is the base path of the versioned API.
const Prefix = "/api/" + Version

// IncidentRow is one row of the incident list. Its field set is fixed by the
// design's route table (DASH-4).
type IncidentRow struct {
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
	Kind        string `json:"kind"`
	Source      string `json:"source"`
	Severity    string `json:"severity"`
	State       string `json:"state"`
	Count       int    `json:"count"`
	FirstSeen   string `json:"first_seen"`
	LastSeen    string `json:"last_seen"`
	Summary     string `json:"summary"`

	// ExplanationPending is true while the model has not answered yet;
	// ExplanationUnavailable is true when the attempt failed. They are what the
	// UI renders as "analysing" / "unavailable" instead of an empty summary
	// (DASH-18).
	ExplanationPending     bool `json:"explanation_pending"`
	ExplanationUnavailable bool `json:"explanation_unavailable"`
}

// IncidentDetail is the list row plus the explanation fields and recent
// occurrence timestamps (DASH-4, DASH-13).
type IncidentDetail struct {
	IncidentRow
	LikelyCause  string   `json:"likely_cause"`
	Evidence     []string `json:"evidence"`
	SuggestedFix string   `json:"suggested_fix"`
	Confidence   float64  `json:"confidence"`
	Model        string   `json:"model"`
	Error        string   `json:"error,omitempty"`
	Occurrences  []string `json:"occurrences"`
}

// Health is the liveness response: status, version, uptime, and counters.
type Health struct {
	Status           string `json:"status"`
	Version          string `json:"version"`
	APIVersion       string `json:"api_version"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
	IncidentsTracked int    `json:"incidents_tracked"`
	Active           int    `json:"active"`
	Resolved         int    `json:"resolved"`

	// Persistent reports whether history survives a restart; false means the API
	// is serving in-memory state after a storage failure (DASH-26).
	Persistent bool `json:"persistent"`
}

// errBody is the shape of every error response, so a client can show a reason
// rather than an HTML page.
type errBody struct {
	Error string `json:"error"`
}

// rowOf maps stored state to the list wire shape.
func rowOf(inc store.Incident) IncidentRow {
	return IncidentRow{
		ID:                     inc.ID,
		Fingerprint:            inc.Fingerprint,
		Kind:                   inc.Kind,
		Source:                 inc.Source,
		Severity:               inc.Severity,
		State:                  string(inc.State),
		Count:                  inc.Count,
		FirstSeen:              stamp(inc.FirstSeen),
		LastSeen:               stamp(inc.LastSeen),
		Summary:                inc.Summary,
		ExplanationPending:     !inc.HasExplanation,
		ExplanationUnavailable: inc.HasExplanation && inc.ExplanationError != "",
	}
}

// detailOf maps stored state to the detail wire shape.
func detailOf(d store.Detail) IncidentDetail {
	out := IncidentDetail{
		IncidentRow: rowOf(d.Incident),
		Evidence:    []string{},
		Occurrences: stamps(d.Occurrences),
	}
	if d.Latest != nil {
		out.LikelyCause = d.Latest.LikelyCause
		out.SuggestedFix = d.Latest.SuggestedFix
		out.Confidence = d.Latest.Confidence
		out.Model = d.Latest.Model
		out.Error = d.Latest.Error
		if d.Latest.Evidence != nil {
			out.Evidence = d.Latest.Evidence
		}
	}
	return out
}

func stamps(times []time.Time) []string {
	out := make([]string, 0, len(times))
	for _, t := range times {
		out = append(out, stamp(t))
	}
	return out
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
