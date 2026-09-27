package detect

import (
	"regexp"
	"strings"
)

// Pattern recognition is deliberately heuristic: logs have no universal format
// and requiring one would make Watcher undeployable anywhere unusual. Triggers
// stay broad and block boundaries are decided by continuation predicates in
// block.go.
var (
	goPanicTrigger    = regexp.MustCompile(`^(panic:|fatal error:)`)
	goGoroutineHeader = regexp.MustCompile(`^goroutine \d+ \[.*\]:`)
	goFrame           = regexp.MustCompile(`^\t.*\.go:\d+(\s+\+0x[0-9a-f]+)?$`)
	exitStatus        = regexp.MustCompile(`^exit status \d+$`)

	pythonTrigger   = regexp.MustCompile(`^Traceback \(most recent call last\):`)
	pythonException = regexp.MustCompile(`^[A-Za-z_][\w.]*(Error|Exception|Warning)?:`)

	genericFatalTrigger = regexp.MustCompile(`\b(FATAL|CRITICAL)\b|^(Unhandled )?\w*(Exception|Error):`)

	// newRecord marks the start of a fresh log record, which is the natural
	// boundary where one crash block ends and unrelated output resumes.
	newRecord = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}|\d\d:\d\d:\d\d|[A-Z][a-z]{2}\s+\d+ \d\d:\d\d:\d\d|\[?(INFO|WARN|WARNING|ERROR|DEBUG|TRACE|FATAL|CRITICAL)\b)`)

	continuationPhrase = regexp.MustCompile(`^(Caused by:|at |\.\.\. \d+ more|Suppressed:)`)

	// goFunctionLine recognizes a Go stack's function-name lines, which sit
	// between the tab-indented file:line frames and carry no indentation.
	goFunctionLine = regexp.MustCompile(`^\S+\(.*\)$|^[\w./*]+$`)
)

// StackShaped reports whether a kind's block is a stack whose lines should be
// reassembled if the source interleaved unrelated output into it.
func StackShaped(kind string) bool {
	return kind == KindGoPanic || kind == KindPythonTraceback
}

// IsBlockContinuation reports whether a line belongs to an already-opened block
// of the given kind. It is the stateless counterpart of the detector's
// continuation predicate, used by context curation to drop interleaved lines
// without duplicating the rules.
func IsBlockContinuation(kind, line string) bool {
	if line == "" {
		return false
	}
	switch kind {
	case KindGoPanic:
		switch {
		case goGoroutineHeader.MatchString(line):
			return true
		case strings.HasPrefix(line, "\t"), goFrame.MatchString(line):
			return true
		case exitStatus.MatchString(line):
			return true
		case goFunctionLine.MatchString(line):
			return true
		default:
			return false
		}
	case KindPythonTraceback:
		return startsWithSpace(line) || pythonException.MatchString(line) || pythonTrigger.MatchString(line)
	default:
		return startsWithSpace(line) || continuationPhrase.MatchString(line)
	}
}

// TriggerKind reports the detector kind a line would start, if any. It is used
// both to open a block and to notice that a new crash has begun mid-block.
func TriggerKind(line string) (string, bool) {
	switch {
	case goPanicTrigger.MatchString(line):
		return KindGoPanic, true
	case pythonTrigger.MatchString(line):
		return KindPythonTraceback, true
	case genericFatalTrigger.MatchString(line):
		return KindGenericFatal, true
	default:
		return "", false
	}
}
