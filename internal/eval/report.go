package eval

import (
	"encoding/json"
	"math"
	"strings"
)

// KindTally is the pass/fail count for one detector kind.
type KindTally struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
}

// CaseRecord is the machine-readable record of one scored case.
type CaseRecord struct {
	ID               string   `json:"id"`
	Category         Category `json:"category"`
	Score            float64  `json:"score"`
	ExpectedCause    string   `json:"expected_cause"`
	ActualCause      *string  `json:"actual_cause"`
	ExpectedKind     string   `json:"expected_kind,omitempty"`
	ActualKind       string   `json:"actual_kind,omitempty"`
	KindMatch        *bool    `json:"kind_match,omitempty"`
	ExplanationError string   `json:"explanation_error,omitempty"`
	Extras           int      `json:"extras,omitempty"`

	passed bool
}

// Report is the whole evaluation result: totals, a per-kind breakdown, every
// case, and the unmatched identifiers on both sides (EVAL-OUT-5, EVAL-OUT-8).
type Report struct {
	SchemaVersion int                  `json:"schema_version"`
	Identifier    string               `json:"identifier"`
	Threshold     float64              `json:"threshold"`
	MinPassRate   *float64             `json:"min_pass_rate,omitempty"`
	Total         int                  `json:"total"`
	Passed        int                  `json:"passed"`
	Failed        int                  `json:"failed"`
	PassRate      float64              `json:"pass_rate"`
	Verdict       string               `json:"verdict"`
	ByKind        map[string]KindTally `json:"by_kind"`
	Cases         []CaseRecord         `json:"cases"`
	Unmatched     Unmatched            `json:"unmatched"`

	Junk int `json:"-"`
}

// noExplanationKey is the extra by-kind bucket that surfaces cases the model
// did not explain, so a weak model is visible even when the detector kind is
// correct (EVAL-OUT-4).
const noExplanationKey = "no_explanation"

// Options are the resolved inputs to one evaluation.
type Options struct {
	Cases       []Case
	Results     []Result
	Identifier  Identifier
	Threshold   float64
	RequireKind bool
	MinPassRate *float64 // nil means report only, with no gate
	Junk        int
}

// Score correlates the cases with the results, classifies each, and builds the
// report. It is pure and offline: the same inputs always produce the same
// report (EVAL-SCORE-7, EVAL-SCORE-8).
func Score(opts Options) Report {
	chosen, unmatched := correlate(opts.Cases, opts.Results, opts.Identifier)

	report := Report{
		SchemaVersion: 1,
		Identifier:    string(opts.Identifier),
		Threshold:     round4(opts.Threshold),
		MinPassRate:   opts.MinPassRate,
		ByKind:        make(map[string]KindTally),
		Unmatched:     unmatched,
		Junk:          opts.Junk,
	}

	for i, c := range opts.Cases {
		outcome := scoreCase(c, chosen[i].Result, opts.Threshold, opts.RequireKind)

		report.Total++
		if outcome.Passed {
			report.Passed++
		}

		kind := kindBucket(c, chosen[i].Result)
		bump(report.ByKind, kind, outcome.Passed)
		if outcome.Category == CategoryNoExplanation {
			bump(report.ByKind, noExplanationKey, outcome.Passed)
		}

		report.Cases = append(report.Cases, recordOf(outcome, len(chosen[i].Extras)))
	}

	report.Failed = report.Total - report.Passed
	report.PassRate = round4(rate(report.Passed, report.Total))
	report.Verdict = verdict(opts.MinPassRate, report.PassRate)
	return report
}

// JSON renders the machine-readable report for CI history (EVAL-OUT-5).
func (r Report) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// kindBucket keys a case by its expected detector kind, falling back to the
// actual kind and then to "unknown", so every case lands in exactly one kind
// row.
func kindBucket(c Case, r *Result) string {
	if c.Kind != "" {
		return c.Kind
	}
	if r != nil && r.Kind != "" {
		return r.Kind
	}
	return "unknown"
}

func bump(counts map[string]KindTally, key string, passed bool) {
	tally := counts[key]
	tally.Total++
	if passed {
		tally.Passed++
	}
	counts[key] = tally
}

func recordOf(o Outcome, extras int) CaseRecord {
	rec := CaseRecord{
		ID:               o.Case.ID,
		Category:         o.Category,
		Score:            round4(o.Score),
		ExpectedCause:    o.Case.ExpectedCause,
		ExplanationError: o.ExplanationError,
		Extras:           extras,
		passed:           o.Passed,
	}
	if o.Result != nil && strings.TrimSpace(o.Result.LikelyCause) != "" {
		cause := o.Result.LikelyCause
		rec.ActualCause = &cause
	}
	if o.ExpectedKind != "" || o.ActualKind != "" {
		rec.ExpectedKind = o.ExpectedKind
		rec.ActualKind = o.ActualKind
		kindMatch := o.KindMatch
		rec.KindMatch = &kindMatch
	}
	return rec
}

func rate(passed, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(passed) / float64(total)
}

// verdict is "pass" when no gate is configured (report only), and otherwise
// depends on whether the pass rate meets the gate (OD-04-2).
func verdict(minPassRate *float64, passRate float64) string {
	if minPassRate == nil || passRate >= *minPassRate {
		return "pass"
	}
	return "fail"
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
