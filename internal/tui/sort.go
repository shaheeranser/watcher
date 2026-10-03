package tui

import (
	"sort"
	"strconv"
	"time"

	"github.com/shaheeranser/watcher/internal/api"
	"github.com/shaheeranser/watcher/internal/store"
)

// sortRows orders the list so the thing currently on fire is at the top:
// unresolved incidents before resolved ones, and most-recent activity first
// within each group (DASH-17). Severity deliberately does not reorder the list,
// so rows do not jump around as severities arrive late (design §4.2).
func sortRows(rows []api.IncidentRow) []api.IncidentRow {
	out := append([]api.IncidentRow(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		resolvedI := out[i].State == string(store.StateResolved)
		resolvedJ := out[j].State == string(store.StateResolved)
		if resolvedI != resolvedJ {
			return !resolvedI
		}
		ti, tj := parseStamp(out[i].LastSeen), parseStamp(out[j].LastSeen)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func indexOfID(rows []api.IncidentRow, id string) int {
	for i, r := range rows {
		if r.ID == id {
			return i
		}
	}
	return -1
}

func parseStamp(raw string) time.Time {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ageOf renders how long ago an incident was last seen, in the compact form the
// list row uses.
func ageOf(raw string, now time.Time) string {
	t := parseStamp(raw)
	if t.IsZero() {
		return "—"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s ago"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	default:
		return strconv.Itoa(int(d.Hours())/24) + "d ago"
	}
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
