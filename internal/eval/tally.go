package eval

import (
	"fmt"
	"sort"
	"strings"
)

// Tally renders the slide-ready text report: totals and the gate verdict, a
// per-detector-kind breakdown, the failures with their expected and actual
// causes, and the unmatched identifiers in both directions (EVAL-OUT-1,
// EVAL-OUT-2, EVAL-OUT-3, EVAL-OUT-4, EVAL-OUT-8).
func (r Report) Tally() string {
	var b strings.Builder

	fmt.Fprintf(&b, "Watcher evaluation — %d cases   (identifier: %s, τ = %.2f)\n", r.Total, r.Identifier, r.Threshold)
	b.WriteString("\n")
	fmt.Fprintf(&b, "  PASS %d / %d   %.1f%%", r.Passed, r.Total, r.PassRate*100)
	if r.MinPassRate == nil {
		b.WriteString("        gate: none (report only)")
	} else {
		fmt.Fprintf(&b, "        gate: >= %.1f%%  →  %s", *r.MinPassRate*100, strings.ToUpper(r.Verdict))
	}
	b.WriteString("\n")

	r.writeKinds(&b)
	r.writeFailures(&b)
	r.writeUnmatched(&b)
	return b.String()
}

func (r Report) writeKinds(b *strings.Builder) {
	b.WriteString("\n  by detector kind\n")
	rows := make([]struct {
		label string
		tally KindTally
	}, 0, len(r.ByKind))
	for kind, tally := range r.ByKind {
		if kind == noExplanationKey {
			continue
		}
		rows = append(rows, struct {
			label string
			tally KindTally
		}{kind, tally})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].label < rows[j].label })
	if tally, ok := r.ByKind[noExplanationKey]; ok {
		rows = append(rows, struct {
			label string
			tally KindTally
		}{"no explanation", tally})
	}

	width := 0
	for _, row := range rows {
		if len(row.label) > width {
			width = len(row.label)
		}
	}
	for _, row := range rows {
		fmt.Fprintf(b, "    %-*s   %d/%-3d %6.1f%%\n", width, row.label,
			row.tally.Passed, row.tally.Total, rate(row.tally.Passed, row.tally.Total)*100)
	}
}

func (r Report) writeFailures(b *strings.Builder) {
	var failed []CaseRecord
	for _, c := range r.Cases {
		if !c.passed {
			failed = append(failed, c)
		}
	}
	fmt.Fprintf(b, "\n  Failures (%d)\n", len(failed))
	for _, c := range failed {
		kind := c.ActualKind
		if kind == "" {
			kind = "—"
		}
		fmt.Fprintf(b, "    %s  %-14s sim %.2f  %s\n", c.ID, kind, c.Score, c.Category)
		fmt.Fprintf(b, "      expected: %s\n", c.ExpectedCause)
		fmt.Fprintf(b, "      actual:   %s\n", actualCause(c))
	}
}

func (r Report) writeUnmatched(b *strings.Builder) {
	if len(r.Unmatched.TruthOnly) == 0 && len(r.Unmatched.ResultsOnly) == 0 {
		return
	}
	b.WriteString("\n  Unmatched\n")
	if len(r.Unmatched.TruthOnly) > 0 {
		fmt.Fprintf(b, "    truth only (no result produced): %s\n", strings.Join(r.Unmatched.TruthOnly, ", "))
	}
	if len(r.Unmatched.ResultsOnly) > 0 {
		fmt.Fprintf(b, "    results only (no truth entry):   %s\n", strings.Join(r.Unmatched.ResultsOnly, ", "))
	}
}

func actualCause(c CaseRecord) string {
	if c.ActualCause != nil {
		return *c.ActualCause
	}
	if c.Category == CategoryNoResult {
		return "(no result produced)"
	}
	return "(explanation unavailable)"
}
