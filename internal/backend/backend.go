// Package backend asks a model to explain a curated excerpt. It owns the
// request and response shape, the prompt, output validation, and evidence
// grounding; it knows nothing about sources, sinks, or notification policy, so
// the model behind it can be swapped without touching the pipeline.
package backend

import "context"

// Explanation is the structured result required of the model. The JSON tags are
// the contract with the model and are reused by the JSONL sink.
type Explanation struct {
	Summary      string   `json:"summary"`
	LikelyCause  string   `json:"likely_cause"`
	Evidence     []string `json:"evidence"`
	SuggestedFix string   `json:"suggested_fix"`
	Confidence   float64  `json:"confidence"`
	Severity     string   `json:"severity"`
}

// Request is one explanation job: the detector kind, the source it came from,
// the already-budgeted excerpt, and the crash fingerprint.
type Request struct {
	Kind        string
	Source      string
	Excerpt     string
	Fingerprint string
}

// Backend explains an excerpt. Implementations must be safe to call
// concurrently up to the pipeline's configured worker limit.
type Backend interface {
	Explain(ctx context.Context, req Request) (Explanation, error)
	Name() string
}
