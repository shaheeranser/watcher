package eval

import (
	"testing"
)

func result(fingerprint, runID, kind, cause, severity string, confidence float64, firstSeen string) Result {
	return Result{
		Fingerprint: fingerprint,
		RunID:       runID,
		Kind:        kind,
		LikelyCause: cause,
		Severity:    severity,
		Confidence:  confidence,
		FirstSeen:   firstSeen,
	}
}

func TestCorrelateFingerprintMode(t *testing.T) {
	cases := []Case{{ID: "abc", ExpectedCause: "nil deref"}, {ID: "missing", ExpectedCause: "x"}}
	results := []Result{result("abc", "", "go-panic", "nil deref", "high", 0.8, "2026-01-02T03:04:05Z")}

	chosen, unmatched := correlate(cases, results, ByFingerprint)
	if chosen[0].Result == nil || chosen[0].Result.Fingerprint != "abc" {
		t.Fatalf("case abc should match: %+v", chosen[0])
	}
	if chosen[1].Result != nil {
		t.Fatalf("case missing should not match: %+v", chosen[1])
	}
	if len(unmatched.TruthOnly) != 1 || unmatched.TruthOnly[0] != "missing" {
		t.Errorf("truth only = %v, want [missing]", unmatched.TruthOnly)
	}
	if len(unmatched.ResultsOnly) != 0 {
		t.Errorf("results only = %v, want none", unmatched.ResultsOnly)
	}
}

func TestCorrelateRunIDMode(t *testing.T) {
	cases := []Case{{ID: "run-014", ExpectedCause: "x"}}
	results := []Result{
		result("fp1", "run-014", "generic-fatal", "x", "high", 0.8, "2026-01-02T03:04:05Z"),
		result("fp2", "run-999", "go-panic", "y", "low", 0.5, "2026-01-02T03:04:05Z"),
	}
	chosen, unmatched := correlate(cases, results, ByRunID)
	if chosen[0].Result == nil || chosen[0].Result.RunID != "run-014" {
		t.Fatalf("run-014 should match its result: %+v", chosen[0])
	}
	if len(unmatched.ResultsOnly) != 1 || unmatched.ResultsOnly[0] != "run-999" {
		t.Errorf("results only = %v, want [run-999]", unmatched.ResultsOnly)
	}
	if len(unmatched.TruthOnly) != 0 {
		t.Errorf("truth only = %v, want none", unmatched.TruthOnly)
	}
}

func TestCorrelateTieBreak(t *testing.T) {
	base := func(severity string, confidence float64, firstSeen string) Result {
		return result("fp", "run-1", "go-panic", "cause", severity, confidence, firstSeen)
	}
	tests := []struct {
		name    string
		bucket  []Result
		wantIdx int
	}{
		{
			name: "highest severity wins",
			bucket: []Result{
				base("low", 0.9, "2026-01-01T00:00:00Z"),
				base("critical", 0.1, "2026-01-02T00:00:00Z"),
			},
			wantIdx: 1,
		},
		{
			name: "then highest confidence",
			bucket: []Result{
				base("high", 0.4, "2026-01-01T00:00:00Z"),
				base("high", 0.9, "2026-01-02T00:00:00Z"),
			},
			wantIdx: 1,
		},
		{
			name: "then earliest first_seen",
			bucket: []Result{
				base("high", 0.5, "2026-01-02T00:00:00Z"),
				base("high", 0.5, "2026-01-01T00:00:00Z"),
			},
			wantIdx: 1,
		},
		{
			name: "unparseable timestamp loses",
			bucket: []Result{
				base("high", 0.5, "not-a-time"),
				base("high", 0.5, "2026-01-02T00:00:00Z"),
			},
			wantIdx: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cases := []Case{{ID: "run-1", ExpectedCause: "cause"}}
			chosen, _ := correlate(cases, tt.bucket, ByRunID)
			if chosen[0].Result == nil {
				t.Fatal("expected a chosen result")
			}
			if *chosen[0].Result != tt.bucket[tt.wantIdx] {
				t.Errorf("chose %+v, want index %d", *chosen[0].Result, tt.wantIdx)
			}
			if len(chosen[0].Extras) != 1 {
				t.Errorf("extras = %d, want 1", len(chosen[0].Extras))
			}
		})
	}
}

func TestCorrelateDropsResultsWithoutAKey(t *testing.T) {
	cases := []Case{{ID: "run-1", ExpectedCause: "x"}}
	results := []Result{result("fp", "", "go-panic", "x", "high", 0.8, "")}
	chosen, unmatched := correlate(cases, results, ByRunID)
	if chosen[0].Result != nil {
		t.Errorf("a result with no run id must not match: %+v", chosen[0])
	}
	if len(unmatched.TruthOnly) != 1 || len(unmatched.ResultsOnly) != 0 {
		t.Errorf("unmatched = %+v, want truth only [run-1]", unmatched)
	}
}

func TestCorrelateResultsOnlyIsSorted(t *testing.T) {
	cases := []Case{{ID: "run-1", ExpectedCause: "x"}}
	results := []Result{
		result("fp", "zzz", "k", "x", "high", 1, ""),
		result("fp", "aaa", "k", "x", "high", 1, ""),
		result("fp", "mmm", "k", "x", "high", 1, ""),
	}
	_, unmatched := correlate(cases, results, ByRunID)
	want := []string{"aaa", "mmm", "zzz"}
	for i, w := range want {
		if unmatched.ResultsOnly[i] != w {
			t.Fatalf("results only = %v, want %v", unmatched.ResultsOnly, want)
		}
	}
}
