package eval

import "strings"

// Category is the classification of one scored case. Every case falls into
// exactly one (design §4.3).
type Category string

const (
	CategoryPass          Category = "pass"
	CategoryCauseMismatch Category = "cause_mismatch"
	CategoryNoExplanation Category = "no_explanation"
	CategoryNoResult      Category = "no_result"
	CategoryKindMismatch  Category = "kind_mismatch"
)

// Outcome is one ground-truth case and its verdict: the result it was scored
// against, the similarity score, and the category.
type Outcome struct {
	Case             Case
	Result           *Result
	Category         Category
	Score            float64
	Passed           bool
	ExpectedKind     string
	ActualKind       string
	KindMatch        bool
	ExplanationError string
}

// scoreCase classifies one matched pair (EVAL-SCORE-1, EVAL-SCORE-5,
// EVAL-SCORE-6). A missing result is a failure with no score; a result whose
// explanation is unavailable is a "no explanation" failure and never reaches
// the similarity metric.
func scoreCase(c Case, r *Result, threshold float64, requireKind bool) Outcome {
	outcome := Outcome{Case: c, Result: r}

	if r == nil {
		outcome.Category = CategoryNoResult
		return outcome
	}
	if r.ExplanationUnavailable || strings.TrimSpace(r.LikelyCause) == "" {
		outcome.Category = CategoryNoExplanation
		outcome.ExplanationError = r.Error
		return outcome
	}

	outcome.Score = similarity(c.ExpectedCause, r.LikelyCause)
	outcome.ExpectedKind = c.Kind
	outcome.ActualKind = r.Kind
	outcome.KindMatch = c.Kind == "" || c.Kind == r.Kind

	switch {
	case outcome.Score < threshold:
		outcome.Category = CategoryCauseMismatch
	case !outcome.KindMatch:
		// A kind mismatch is recorded separately from a cause mismatch because
		// "the detector labeled it differently" is a different bug than
		// "Watcher misdiagnosed the cause" (EVAL-SCORE-6). It fails only when
		// the caller asked for strict kinds (OD-04-5).
		outcome.Category = CategoryKindMismatch
		outcome.Passed = !requireKind
	default:
		outcome.Category = CategoryPass
		outcome.Passed = true
	}
	return outcome
}
