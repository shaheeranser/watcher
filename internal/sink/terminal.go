package sink

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Terminal renders each result as a human-readable block. Color is applied only
// when the caller opted in, so a terminal that does not want escape codes (or
// has NO_COLOR set) still gets clean text (CORE-OUT-1).
type Terminal struct {
	mu    sync.Mutex
	w     io.Writer
	color bool
}

func NewTerminal(w io.Writer, color bool) *Terminal {
	return &Terminal{w: w, color: color}
}

func (s *Terminal) Name() string { return "terminal" }

func (s *Terminal) Emit(_ context.Context, r Result) error {
	summary, cause, evidence, fix, confidence, severity := fields(r)

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s count=%d\n", s.label(severityLabel(severity)), r.Fingerprint, r.Count)
	fmt.Fprintf(&b, "source: %s\n", r.Source)
	fmt.Fprintf(&b, "first seen: %s\n", r.FirstSeen.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "last seen:  %s\n", r.LastSeen.UTC().Format(time.RFC3339))

	if r.Explanation == nil {
		reason := r.ExplainErr
		if reason == "" {
			reason = "no explanation available"
		}
		fmt.Fprintf(&b, "explanation unavailable: %s\n\n", reason)
		return s.write(b.String())
	}

	fmt.Fprintf(&b, "model: %s\n", r.Model)
	fmt.Fprintf(&b, "summary: %s\n", summary)
	fmt.Fprintf(&b, "likely cause: %s\n", cause)
	if len(evidence) > 0 {
		b.WriteString("evidence:\n")
		for _, e := range evidence {
			fmt.Fprintf(&b, "  - %q\n", e)
		}
	}
	fmt.Fprintf(&b, "suggested fix: %s\n", fix)
	fmt.Fprintf(&b, "confidence: %.2f\n\n", confidence)
	return s.write(b.String())
}

func (s *Terminal) write(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := io.WriteString(s.w, text)
	return err
}

func severityLabel(severity string) string {
	if severity == "" {
		return "UNKNOWN"
	}
	return strings.ToUpper(severity)
}

func (s *Terminal) label(severity string) string {
	if !s.color {
		return "[" + severity + "]"
	}
	return "\x1b[" + severityColor(severity) + "m" + severity + "\x1b[0m"
}

func severityColor(severity string) string {
	switch severity {
	case "CRITICAL":
		return "1;31"
	case "HIGH":
		return "31"
	case "MEDIUM":
		return "33"
	case "LOW":
		return "32"
	default:
		return "0"
	}
}
