package eval

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrNoIncidents is returned when the results input parses but contains no
// incidents at all, so the caller can report that distinctly rather than
// reporting a 0% score (EVAL-ACT-4).
var ErrNoIncidents = errors.New("results input contains no incidents")

// maxLineBytes bounds one results line. An evidence array can make a line far
// larger than bufio's default, so the scanner buffer is raised rather than
// silently truncating an incident.
const maxLineBytes = 1 << 20

// Result is one parsed incident from Watcher's JSON Lines stream. Unknown
// fields are ignored, so a results file from a newer build still parses
// (EVAL-ACT-3).
type Result struct {
	Fingerprint            string
	RunID                  string
	Kind                   string
	LikelyCause            string
	Severity               string
	Confidence             float64
	FirstSeen              string
	ExplanationUnavailable bool
	Error                  string
}

// Results is the parsed incident stream plus the count of lines that were not
// JSON at all.
type Results struct {
	Incidents []Result
	Junk      int
}

// markerKeys identify a line as a Watcher result. A valid JSON object carrying
// none of them is not an incident and is skipped, not counted as junk.
var markerKeys = []string{"fingerprint", "likely_cause", "explanation_unavailable"}

// ReadResults streams a results file line by line (EVAL-NFR-2): blank lines are
// skipped, unparseable lines are counted and reported, and non-incident JSON is
// ignored. An input with no incidents is a distinct error (EVAL-ACT-4).
func ReadResults(r io.Reader) (Results, error) {
	var out Results
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &keys); err != nil {
			out.Junk++
			continue
		}
		if !isIncident(keys) {
			continue
		}
		var rec resultLine
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			out.Junk++
			continue
		}
		out.Incidents = append(out.Incidents, rec.result())
	}
	if err := scanner.Err(); err != nil {
		return Results{}, fmt.Errorf("read results: %w", err)
	}
	if len(out.Incidents) == 0 {
		return Results{}, ErrNoIncidents
	}
	return out, nil
}

// resultLine mirrors the documented JSONL field set. It is unexported because
// Result is the value the rest of the package works with.
type resultLine struct {
	Fingerprint            string  `json:"fingerprint"`
	RunID                  string  `json:"run_id"`
	Kind                   string  `json:"kind"`
	LikelyCause            string  `json:"likely_cause"`
	Severity               string  `json:"severity"`
	Confidence             float64 `json:"confidence"`
	FirstSeen              string  `json:"first_seen"`
	ExplanationUnavailable bool    `json:"explanation_unavailable"`
	Error                  string  `json:"error"`
}

func (l resultLine) result() Result {
	return Result{
		Fingerprint:            l.Fingerprint,
		RunID:                  l.RunID,
		Kind:                   l.Kind,
		LikelyCause:            l.LikelyCause,
		Severity:               l.Severity,
		Confidence:             l.Confidence,
		FirstSeen:              l.FirstSeen,
		ExplanationUnavailable: l.ExplanationUnavailable,
		Error:                  l.Error,
	}
}

func isIncident(keys map[string]json.RawMessage) bool {
	for _, key := range markerKeys {
		if _, ok := keys[key]; ok {
			return true
		}
	}
	return false
}
