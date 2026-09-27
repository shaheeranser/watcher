package fingerprint

import (
	"regexp"
	"strings"

	"github.com/shaheeranser/watcher/internal/source"
)

// Normalize reduces a block to the text that identifies the crash rather than
// the occurrence: volatile tokens become stable placeholders so the same crash
// seen twice hashes alike (CORE-FP-2, CORE-FP-3). The output is also returned
// on the fingerprint for debugging.
func Normalize(kind string, block []source.Line) string {
	raws := make([]string, len(block))
	for i, l := range block {
		raws[i] = l.Raw
	}
	return normalizeText(strings.Join(raws, "\n"))
}

// The order is load-bearing: each rule assumes the ones above it have already
// removed the tokens that would otherwise be swallowed by a broader pattern.
// Changing the order changes every fingerprint.
var rules = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`0x[0-9a-fA-F]+\b`), "0xADDR"},
	{regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`), "<UUID>"},
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?|\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(\.\d+)?|[A-Z][a-z]{2}\s+\d+ \d\d:\d\d:\d\d`), "<TS>"},
	{regexp.MustCompile(`(?i)\b(request[_-]?id|req[_-]?id|trace[_-]?id|span[_-]?id|x-request-id)=\S+`), "<ID>"},
	{regexp.MustCompile(`goroutine \d+`), "goroutine N"},
	{regexp.MustCompile(`\b(pid|tid)[=: ]\s*\d+`), "<PID>"},
	{regexp.MustCompile(`\[\d+\]`), "<PID>"},
	{regexp.MustCompile(`\d+(\.\d+)?\s?(ns|µs|us|ms|s)\b`), "<DUR>"},
	{regexp.MustCompile(`/tmp/[\w.\-]+|/var/folders/[\w/.\-]+`), "<TMP>"},
	{regexp.MustCompile(`:\d{4,5}\b`), ":PORT"},
	{regexp.MustCompile(`\d+(\.\d+)?\s?(B|KB|MB|GB)\b`), "<SIZE>"},
}

var whitespaceRun = regexp.MustCompile(`\s+`)

func normalizeText(text string) string {
	for _, r := range rules {
		text = r.pattern.ReplaceAllString(text, r.replacement)
	}

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(whitespaceRun.ReplaceAllString(line, " "), " ")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
