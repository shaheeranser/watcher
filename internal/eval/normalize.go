package eval

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// stopwords are dropped before comparison so "the config loader failed" and
// "config loader failure" are not pulled apart by filler words (design §4.1).
var stopwords = map[string]bool{
	"a": true, "the": true, "in": true, "of": true, "on": true,
	"at": true, "to": true, "is": true, "was": true,
}

// normalize reduces a free-text cause to the form the similarity metric
// compares: NFKC, lowercase, punctuation collapsed to spaces, whitespace
// collapsed, and stopwords dropped (EVAL-SCORE-2).
func normalize(text string) string {
	text = norm.NFKC.String(text)
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || unicode.IsSpace(r) {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteByte(' ')
	}
	tokens := strings.Fields(b.String())
	kept := tokens[:0]
	for _, tok := range tokens {
		if !stopwords[tok] {
			kept = append(kept, tok)
		}
	}
	return strings.Join(kept, " ")
}
