package sink

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// JSONL emits one compact JSON object per result. It is the default when stdout
// is not a terminal, so a pipe or redirect yields a machine-readable stream
// (CORE-OUT-2, CORE-OUT-3).
type JSONL struct {
	mu sync.Mutex
	w  io.Writer
}

func NewJSONL(w io.Writer) *JSONL {
	return &JSONL{w: w}
}

func (s *JSONL) Name() string { return "jsonl" }

func (s *JSONL) Emit(_ context.Context, r Result) error {
	line, err := json.Marshal(jsonRecordFrom(r))
	if err != nil {
		return err
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.w.Write(line)
	return err
}

// jsonRecord is the wire shape. The field set is fixed by CORE-OUT-3; the two
// trailing fields are additive and only appear when the explanation failed.
type jsonRecord struct {
	Fingerprint            string   `json:"fingerprint"`
	Kind                   string   `json:"kind"`
	Source                 string   `json:"source"`
	Count                  int      `json:"count"`
	FirstSeen              string   `json:"first_seen"`
	LastSeen               string   `json:"last_seen"`
	Summary                string   `json:"summary"`
	LikelyCause            string   `json:"likely_cause"`
	Evidence               []string `json:"evidence"`
	SuggestedFix           string   `json:"suggested_fix"`
	Confidence             float64  `json:"confidence"`
	Severity               string   `json:"severity"`
	Model                  string   `json:"model"`
	ExplanationUnavailable bool     `json:"explanation_unavailable,omitempty"`
	Error                  string   `json:"error,omitempty"`
}

func jsonRecordFrom(r Result) jsonRecord {
	summary, cause, evidence, fix, confidence, severity := fields(r)
	if evidence == nil {
		evidence = []string{}
	}
	record := jsonRecord{
		Fingerprint:  r.Fingerprint,
		Kind:         r.Kind,
		Source:       r.Source,
		Count:        r.Count,
		FirstSeen:    r.FirstSeen.UTC().Format(time.RFC3339),
		LastSeen:     r.LastSeen.UTC().Format(time.RFC3339),
		Summary:      summary,
		LikelyCause:  cause,
		Evidence:     evidence,
		SuggestedFix: fix,
		Confidence:   confidence,
		Severity:     severity,
		Model:        r.Model,
	}
	if r.Explanation == nil {
		record.ExplanationUnavailable = true
		record.Error = r.ExplainErr
	}
	return record
}
