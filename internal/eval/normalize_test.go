package eval

import (
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercases", "Nil Pointer Dereference", "nil pointer dereference"},
		{"strips punctuation", "nil-pointer, dereference!", "nil pointer dereference"},
		{"collapses whitespace", "  nil \t pointer\n deref  ", "nil pointer deref"},
		{"drops stopwords", "the handler is a nil of request", "handler nil request"},
		{"keeps digits and underscore", "http_500 error", "http_500 error"},
		{"NFKC compatibility", "ＡＢＣ ﬁle", "abc file"},
		{"unicode letters are kept", "café ✦ error", "café error"},
		{"empty stays empty", "  ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalize(tt.input); got != tt.want {
				t.Errorf("normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeIsIdempotent(t *testing.T) {
	property := func(s string) bool {
		once := normalize(s)
		return normalize(once) == once
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 800}); err != nil {
		t.Error(err)
	}
}

type arbitraryText string

func (arbitraryText) Generate(r *rand.Rand, size int) reflect.Value {
	alphabet := []rune("abcXYZ 019_,.!?-\t\nﬁüéＡβ")
	n := r.Intn(24)
	runes := make([]rune, 0, n)
	for i := 0; i < n; i++ {
		runes = append(runes, alphabet[r.Intn(len(alphabet))])
	}
	return reflect.ValueOf(arbitraryText(string(runes)))
}

func TestNormalizeIsStableUnderArbitraryText(t *testing.T) {
	property := func(s arbitraryText) bool {
		once := normalize(string(s))
		return normalize(once) == once
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Error(err)
	}
}
