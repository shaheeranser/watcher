// Package sink delivers finished results. A Sink knows how to render one
// result and nothing about detection or the model, so a new destination is a
// new file here rather than a change to the pipeline.
package sink

import (
	"context"
	"time"

	"github.com/shaheeranser/watcher/internal/backend"
)

// Result is a finished incident report: the crash identity and counters, plus
// the explanation or the reason it is missing.
type Result struct {
	Fingerprint string
	Kind        string
	Source      string
	Count       int
	FirstSeen   time.Time
	LastSeen    time.Time
	Explanation *backend.Explanation
	ExplainErr  string
	Model       string

	// Notification is the lifecycle kind (new/ongoing/resolved) on the
	// notification channel. It is empty on the local result stream, which
	// reports every occurrence rather than the notification policy.
	Notification string

	// Pending marks a notification sent before the model returned, so a
	// receiver can tell "not explained yet" from "will never be explained".
	Pending bool
}

// Sink emits a result. Emit must be safe for concurrent use and must not write
// to stdout other than the result itself (CORE-OUT-4).
type Sink interface {
	Emit(ctx context.Context, r Result) error
	Name() string
}

// fields flattens the explanation so both renderers share one extraction.
func fields(r Result) (summary, cause string, evidence []string, fix string, confidence float64, severity string) {
	if r.Explanation == nil {
		return "", "", nil, "", 0, ""
	}
	e := r.Explanation
	return e.Summary, e.LikelyCause, e.Evidence, e.SuggestedFix, e.Confidence, e.Severity
}
