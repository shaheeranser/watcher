package eval

import "testing"

func TestScoreCaseCategories(t *testing.T) {
	cause := "nil pointer dereference in config loader"
	tests := []struct {
		name        string
		c           Case
		result      *Result
		threshold   float64
		requireKind bool
		wantCat     Category
		wantPassed  bool
	}{
		{
			name:       "exact match passes",
			c:          Case{ID: "a", ExpectedCause: cause},
			result:     &Result{LikelyCause: cause, Kind: "go-panic", Severity: "high"},
			threshold:  0.6,
			wantCat:    CategoryPass,
			wantPassed: true,
		},
		{
			name:       "paraphrase above threshold passes",
			c:          Case{ID: "a", ExpectedCause: cause},
			result:     &Result{LikelyCause: "config loader dereferenced a nil pointer", Kind: "go-panic"},
			threshold:  0.6,
			wantCat:    CategoryPass,
			wantPassed: true,
		},
		{
			name:       "below threshold is a cause mismatch",
			c:          Case{ID: "a", ExpectedCause: cause},
			result:     &Result{LikelyCause: "database connection refused", Kind: "go-panic"},
			threshold:  0.6,
			wantCat:    CategoryCauseMismatch,
			wantPassed: false,
		},
		{
			name:       "unavailable explanation is no_explanation",
			c:          Case{ID: "a", ExpectedCause: cause},
			result:     &Result{Kind: "generic-fatal", ExplanationUnavailable: true, Error: "ollama: no response"},
			threshold:  0.6,
			wantCat:    CategoryNoExplanation,
			wantPassed: false,
		},
		{
			name:       "empty likely_cause is no_explanation",
			c:          Case{ID: "a", ExpectedCause: cause},
			result:     &Result{Kind: "generic-fatal", LikelyCause: "   "},
			threshold:  0.6,
			wantCat:    CategoryNoExplanation,
			wantPassed: false,
		},
		{
			name:       "no result produced is no_result",
			c:          Case{ID: "a", ExpectedCause: cause},
			result:     nil,
			threshold:  0.6,
			wantCat:    CategoryNoResult,
			wantPassed: false,
		},
		{
			name:       "kind mismatch recorded and passes without require-kind",
			c:          Case{ID: "a", ExpectedCause: cause, Kind: "go-panic"},
			result:     &Result{LikelyCause: cause, Kind: "generic-fatal"},
			threshold:  0.6,
			wantCat:    CategoryKindMismatch,
			wantPassed: true,
		},
		{
			name:        "kind mismatch fails under require-kind",
			c:           Case{ID: "a", ExpectedCause: cause, Kind: "go-panic"},
			result:      &Result{LikelyCause: cause, Kind: "generic-fatal"},
			threshold:   0.6,
			requireKind: true,
			wantCat:     CategoryKindMismatch,
			wantPassed:  false,
		},
		{
			name:       "kind matches",
			c:          Case{ID: "a", ExpectedCause: cause, Kind: "go-panic"},
			result:     &Result{LikelyCause: cause, Kind: "go-panic"},
			threshold:  0.6,
			wantCat:    CategoryPass,
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scoreCase(tt.c, tt.result, tt.threshold, tt.requireKind)
			if got.Category != tt.wantCat {
				t.Errorf("category = %q, want %q", got.Category, tt.wantCat)
			}
			if got.Passed != tt.wantPassed {
				t.Errorf("passed = %v, want %v", got.Passed, tt.wantPassed)
			}
		})
	}
}

func TestScoreCaseThresholdBoundary(t *testing.T) {
	expected := "nil pointer dereference"
	actual := "nil pointer dereference in loader"
	raw := similarity(expected, actual)
	if raw <= 0 || raw >= 1 {
		t.Fatalf("test needs a partial score, got %v", raw)
	}

	c := Case{ID: "a", ExpectedCause: expected}
	r := &Result{LikelyCause: actual, Kind: "go-panic"}

	atThreshold := scoreCase(c, r, raw, false)
	if atThreshold.Category != CategoryPass {
		t.Errorf("score at tau should pass, got %q", atThreshold.Category)
	}

	above := scoreCase(c, r, raw+0.01, false)
	if above.Category != CategoryCauseMismatch {
		t.Errorf("score just below tau should fail, got %q", above.Category)
	}

	// The score is recorded independently of the verdict so a stored run can be
	// re-thresholded (EVAL-SCORE-4).
	if above.Score != atThreshold.Score {
		t.Errorf("score changed with the threshold: %v vs %v", above.Score, atThreshold.Score)
	}
}

func TestScoreCaseIsDeterministic(t *testing.T) {
	c := Case{ID: "a", ExpectedCause: "nil pointer dereference in config loader", Kind: "go-panic"}
	r := &Result{LikelyCause: "nil dereference in config loader", Kind: "go-panic"}
	first := scoreCase(c, r, 0.6, false)
	for i := 0; i < 50; i++ {
		got := scoreCase(c, r, 0.6, false)
		if got != first {
			t.Fatalf("scoreCase is not deterministic: %+v vs %+v", got, first)
		}
	}
}
