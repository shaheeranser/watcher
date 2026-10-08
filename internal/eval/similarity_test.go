package eval

import (
	"math"
	"testing"
)

func TestSimilarity(t *testing.T) {
	tests := []struct {
		name        string
		expected    string
		actual      string
		wantAtLeast float64
		wantAtMost  float64
	}{
		{"exact", "nil pointer dereference", "nil pointer dereference", 1, 1},
		{"normalized exact", "Nil Pointer Dereference!", "nil pointer dereference", 1, 1},
		{"stopword and case only", "the config loader failed", "config loader failed", 1, 1},
		{"word order", "config loader nil deref", "nil deref config loader", 0.99, 1},
		{"one side longer", "nil pointer dereference in config loader", "nil pointer dereference", 0.7, 1},
		{"genuinely different", "database connection refused", "nil pointer dereference", 0, 0.2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := similarity(tt.expected, tt.actual)
			if got < tt.wantAtLeast-tolerance || got > tt.wantAtMost+tolerance {
				t.Errorf("similarity(%q, %q) = %.3f, want in [%.2f, %.2f]",
					tt.expected, tt.actual, got, tt.wantAtLeast, tt.wantAtMost)
			}
		})
	}
}

const tolerance = 1e-9

func TestSimilarityIsSymmetric(t *testing.T) {
	a := "nil pointer dereference in the config loader"
	b := "the config loader dereferenced nil"
	if math.Abs(similarity(a, b)-similarity(b, a)) > tolerance {
		t.Errorf("similarity is not symmetric: %v vs %v", similarity(a, b), similarity(b, a))
	}
}

func TestSimilarityEmpty(t *testing.T) {
	if got := similarity("", "anything"); got != 0 {
		t.Errorf("empty expected = %v, want 0", got)
	}
	if got := similarity("anything", ""); got != 0 {
		t.Errorf("empty actual = %v, want 0", got)
	}
	if got := similarity("", ""); got != 1 {
		t.Errorf("both empty are equal = %v, want 1", got)
	}
}

func TestSimilarityMorphologicalVariation(t *testing.T) {
	// Trigrams keep "loading"/"loads"/"loaded" close even when the token sets
	// barely overlap; the max of the two measures is what makes this robust.
	same := similarity("failed to load the config", "config loading failed")
	if same < 0.5 {
		t.Errorf("morphological variation scored too low: %.3f", same)
	}

	// A short token and its plural have identical trigrams apart from the tail.
	plural := similarity("timeout", "timeouts")
	if plural < 0.6 {
		t.Errorf("singular/plural scored too low: %.3f", plural)
	}
}
