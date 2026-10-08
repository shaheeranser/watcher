package eval

import (
	"math"
	"strings"
)

// similarity scores two free-text causes as the maximum of two complementary
// measures: Sørensen–Dice over normalized token sets (tolerant of word order
// and of one side using more words), and Dice over character trigrams (tolerant
// of morphology). An exact normalized match always scores 1.0 (design §4.2,
// EVAL-SCORE-3).
func similarity(expected, actual string) float64 {
	normExpected := normalize(expected)
	normActual := normalize(actual)
	if normExpected == normActual {
		return 1
	}
	if normExpected == "" || normActual == "" {
		return 0
	}
	tokenScore := dice(tokenSet(normExpected), tokenSet(normActual))
	gramScore := dice(trigramSet(normExpected), trigramSet(normActual))
	return math.Max(tokenScore, gramScore)
}

func dice(left, right map[string]struct{}) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	shared := 0
	for item := range left {
		if _, ok := right[item]; ok {
			shared++
		}
	}
	return 2 * float64(shared) / float64(len(left)+len(right))
}

func tokenSet(text string) map[string]struct{} {
	fields := strings.Fields(text)
	set := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		set[f] = struct{}{}
	}
	return set
}

func trigramSet(text string) map[string]struct{} {
	compact := strings.ReplaceAll(text, " ", "")
	runes := []rune(compact)
	set := make(map[string]struct{})
	for i := 0; i+3 <= len(runes); i++ {
		set[string(runes[i:i+3])] = struct{}{}
	}
	return set
}
