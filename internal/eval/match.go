package eval

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Identifier selects the result field ground truth is correlated on.
type Identifier string

const (
	ByFingerprint Identifier = "fingerprint"
	ByRunID       Identifier = "run_id"
)

// ParseIdentifier validates an explicit --identifier value.
func ParseIdentifier(raw string) (Identifier, error) {
	switch Identifier(strings.ToLower(strings.TrimSpace(raw))) {
	case ByFingerprint:
		return ByFingerprint, nil
	case ByRunID:
		return ByRunID, nil
	default:
		return "", fmt.Errorf("unknown identifier %q: want fingerprint or run_id", raw)
	}
}

func (id Identifier) key(r Result) string {
	if id == ByRunID {
		return r.RunID
	}
	return r.Fingerprint
}

// Unmatched records the identifiers present on only one side: truth entries
// with no result, and results with no truth entry (EVAL-MATCH-3, EVAL-MATCH-4).
type Unmatched struct {
	TruthOnly   []string `json:"truth_only"`
	ResultsOnly []string `json:"results_only"`
}

// Chosen is the result scored for one ground-truth case, plus the extra results
// that shared its identifier but were not scored.
type Chosen struct {
	Result *Result
	Extras []Result
}

// correlate maps each case to its chosen result and collects the unmatched
// identifiers in both directions. A result with no correlation key is dropped:
// it cannot be matched, which surfaces as truth-only/no-result instead
// (EVAL-MATCH-1, EVAL-MATCH-3, EVAL-MATCH-4, EVAL-MATCH-5).
func correlate(cases []Case, results []Result, id Identifier) ([]Chosen, Unmatched) {
	byKey := make(map[string][]Result)
	for _, r := range results {
		key := id.key(r)
		if key == "" {
			continue
		}
		byKey[key] = append(byKey[key], r)
	}

	chosen := make([]Chosen, len(cases))
	truthKeys := make(map[string]bool, len(cases))
	var truthOnly []string
	for i, c := range cases {
		truthKeys[c.ID] = true
		bucket := byKey[c.ID]
		if len(bucket) == 0 {
			truthOnly = append(truthOnly, c.ID)
			continue
		}
		best := 0
		for j := 1; j < len(bucket); j++ {
			if better(bucket[j], bucket[best]) {
				best = j
			}
		}
		extras := make([]Result, 0, len(bucket)-1)
		for j, r := range bucket {
			if j != best {
				extras = append(extras, r)
			}
		}
		chosen[i] = Chosen{Result: &bucket[best], Extras: extras}
	}

	var resultsOnly []string
	for key := range byKey {
		if !truthKeys[key] {
			resultsOnly = append(resultsOnly, key)
		}
	}
	sort.Strings(resultsOnly)

	return chosen, Unmatched{TruthOnly: truthOnly, ResultsOnly: resultsOnly}
}

// better reports whether a should be chosen over b when one identifier maps to
// several results: highest severity, then highest confidence, then earliest
// first_seen (OD-04-4). Ties keep arrival order, so the choice is deterministic.
func better(a, b Result) bool {
	if ra, rb := severityRank(a.Severity), severityRank(b.Severity); ra != rb {
		return ra > rb
	}
	if a.Confidence != b.Confidence {
		return a.Confidence > b.Confidence
	}
	ta, okA := parseStamp(a.FirstSeen)
	tb, okB := parseStamp(b.FirstSeen)
	if okA != okB {
		return okA
	}
	if okA && !ta.Equal(tb) {
		return ta.Before(tb)
	}
	return false
}

func severityRank(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func parseStamp(raw string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
