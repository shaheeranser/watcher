// Package incident tracks crashes for the process lifetime: how often each
// fingerprint has been seen, when it first and last appeared, and whether its
// explanation is still fresh. It is what turns a crash loop into one model call
// with a rising count instead of a call per occurrence.
package incident

import (
	"time"

	"github.com/shaheeranser/watcher/internal/backend"
)

// Incident is the accumulated state of one fingerprint on one labeled source.
type Incident struct {
	Fingerprint string
	Kind        string

	// Source is the source label, giving the incident its origin.
	Source          string
	Count           int
	FirstSeen       time.Time
	LastSeen        time.Time
	LastExplainedAt time.Time
	Explanation     *backend.Explanation
	ExplainErr      string
	Model           string
}
