package fingerprint

import (
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/quick"

	"github.com/shaheeranser/watcher/internal/source"
)

func linesFrom(text string) []source.Line {
	parts := strings.Split(text, "\n")
	lines := make([]source.Line, len(parts))
	for i, p := range parts {
		lines[i] = source.Line{Raw: p}
	}
	return lines
}

func TestRewriteRules(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"hex address", "addr 0x1a2b in frame", "addr 0xADDR in frame"},
		{"uuid", "id 550e8400-e29b-41d4-a716-446655440000 end", "id <UUID> end"},
		{"rfc3339", "at 2026-01-02T03:04:05Z done", "at <TS> done"},
		{"datetime", "at 2026-01-02 03:04:05 done", "at <TS> done"},
		{"syslog time", "Jan  2 15:04:05 host", "<TS> host"},
		{"request id", "request_id=abc123 handled", "<ID> handled"},
		{"trace id", "trace_id=xyz789 done", "<ID> done"},
		{"goroutine", "goroutine 17 [running]:", "goroutine N [running]:"},
		{"pid", "pid=1234 start", "<PID> start"},
		{"bracket pid", "[4567] boot", "<PID> boot"},
		{"duration", "took 12.5ms", "took <DUR>"},
		{"temp path", "open /tmp/xyz.log", "open <TMP>"},
		{"port", "listen :8080", "listen :PORT"},
		{"size", "used 512MB", "used <SIZE>"},
		{"collapse whitespace", "a\t\t b", "a b"},
		{"drop empty lines", "a\n\n\nb", "a\nb"},
		{"strip trailing space", "trailing   ", "trailing"},
		{"normalized hex is stable", "already 0xADDR here", "already 0xADDR here"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeText(tt.input); got != tt.want {
				t.Errorf("normalizeText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSameCrashCollapses(t *testing.T) {
	a := linesFrom("panic: request 550e8400-e29b-41d4-a716-446655440000 at 2026-01-02T03:04:05Z failed\n\t/app/main.go:10 +0x1a")
	b := linesFrom("panic: request 11111111-2222-3333-4444-555555555555 at 2027-12-31T23:59:59Z failed\n\t/app/main.go:10 +0xffff")
	if Of("go-panic", a).Hash != Of("go-panic", b).Hash {
		t.Errorf("occurrences differing only in volatile tokens must share a fingerprint")
	}
}

func TestDifferentRootCauseDiffers(t *testing.T) {
	a := linesFrom("panic: nil pointer dereference\n\t/app/main.go:10 +0x1a")
	b := linesFrom("panic: assignment to entry in nil map\n\t/app/main.go:10 +0x1a")
	if Of("go-panic", a).Hash == Of("go-panic", b).Hash {
		t.Errorf("different root causes must not share a fingerprint")
	}
}

func TestKindIsPartOfFingerprint(t *testing.T) {
	block := linesFrom("boom: something failed")
	if Of("go-panic", block).Hash == Of("generic-fatal", block).Hash {
		t.Errorf("the detector kind must affect the fingerprint")
	}
}

func TestNormalizeIsIdempotent(t *testing.T) {
	property := func(s string) bool {
		once := normalizeText(s)
		return normalizeText(once) == once
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 800}); err != nil {
		t.Error(err)
	}
}

func TestFingerprintIsDeterministic(t *testing.T) {
	property := func(s string) bool {
		block := linesFrom(s)
		return Of("k", block) == Of("k", block)
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Error(err)
	}
}

const marker = "MARK"

type volatileCase struct {
	tmpl string
	a    string
	b    string
}

type tokenClass struct {
	tmpl     string
	newToken func(r *rand.Rand) string
}

var tokenClasses = []tokenClass{
	{"fault at " + marker + " in frame", func(r *rand.Rand) string { return fmt.Sprintf("0x%x", r.Uint32()) }},
	{"trace " + marker + " done", func(r *rand.Rand) string { return randomUUID(r) }},
	{"at " + marker + " done", func(r *rand.Rand) string {
		return fmt.Sprintf("2026-%02d-%02dT%02d:%02d:%02dZ", 1+r.Intn(12), 1+r.Intn(28), r.Intn(24), r.Intn(60), r.Intn(60))
	}},
	{"request_id=" + marker + " handled", func(r *rand.Rand) string { return randomWord(r) }},
	{"goroutine " + marker + " [running]:", func(r *rand.Rand) string { return strconv.Itoa(r.Intn(1_000_000)) }},
	{"pid=" + marker + " boot", func(r *rand.Rand) string { return strconv.Itoa(r.Intn(65_535)) }},
	{"[" + marker + "] boot", func(r *rand.Rand) string { return strconv.Itoa(r.Intn(65_535)) }},
	{"took " + marker, func(r *rand.Rand) string { return fmt.Sprintf("%dms", r.Intn(10_000)) }},
	{"open /tmp/" + marker, func(r *rand.Rand) string { return randomWord(r) }},
	{"listen :" + marker, func(r *rand.Rand) string { return strconv.Itoa(1024 + r.Intn(64_000)) }},
	{"used " + marker, func(r *rand.Rand) string { return fmt.Sprintf("%dMB", r.Intn(4096)) }},
}

func (volatileCase) Generate(r *rand.Rand, _ int) reflect.Value {
	c := tokenClasses[r.Intn(len(tokenClasses))]
	a := c.newToken(r)
	b := c.newToken(r)
	for b == a {
		b = c.newToken(r)
	}
	return reflect.ValueOf(volatileCase{tmpl: c.tmpl, a: a, b: b})
}

func TestVolatileTokensCollapse(t *testing.T) {
	property := func(v volatileCase) bool {
		na := normalizeText(strings.Replace(v.tmpl, marker, v.a, 1))
		nb := normalizeText(strings.Replace(v.tmpl, marker, v.b, 1))
		// The token must actually be replaced, otherwise equal output would be
		// meaningless.
		return na == nb && !strings.Contains(na, v.a)
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Error(err)
	}
}

type distinctPair struct {
	a string
	b string
}

func (distinctPair) Generate(r *rand.Rand, _ int) reflect.Value {
	a := randomIdentifier(r)
	b := randomIdentifier(r)
	for b == a {
		b = randomIdentifier(r)
	}
	return reflect.ValueOf(distinctPair{a: a, b: b})
}

func TestDistinctRootCausesStayDistinct(t *testing.T) {
	property := func(p distinctPair) bool {
		return normalizeText(p.a+": boom") != normalizeText(p.b+": boom")
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Error(err)
	}
}

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randomIdentifier(r *rand.Rand) string {
	n := 3 + r.Intn(8)
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[r.Intn(len(letters))]
	}
	return string(b)
}

func randomWord(r *rand.Rand) string {
	// Letters only: a digit-bearing id can incidentally match a broader rule
	// (an id ending in "9s" looks like a duration), which would make the
	// same-class invariance property test the wrong thing.
	const chars = "abcdefghijklmnopqrstuvwxyz"
	n := 3 + r.Intn(8)
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[r.Intn(len(chars))]
	}
	return string(b)
}

func randomUUID(r *rand.Rand) string {
	b := make([]byte, 16)
	r.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
