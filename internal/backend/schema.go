package backend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var validSeverities = map[string]bool{
	"low":      true,
	"medium":   true,
	"high":     true,
	"critical": true,
}

// parseExplanation extracts the JSON object from model output and enforces the
// schema client-side. Client-side validation is required regardless of whether
// the server constrains output, because a small model will sometimes ignore the
// requested format (CORE-BE-3, CORE-BE-4).
func parseExplanation(raw []byte) (Explanation, error) {
	object := extractJSONObject(raw)
	if object == nil {
		return Explanation{}, errors.New("model output contains no JSON object")
	}

	var e Explanation
	if err := json.Unmarshal(object, &e); err != nil {
		return Explanation{}, fmt.Errorf("model output is not valid JSON: %w", err)
	}
	if err := e.validate(); err != nil {
		return Explanation{}, err
	}
	return e, nil
}

func (e Explanation) validate() error {
	if strings.TrimSpace(e.Summary) == "" {
		return errors.New("summary is empty")
	}
	if !validSeverities[e.Severity] {
		return fmt.Errorf("severity %q is not one of low, medium, high, critical", e.Severity)
	}
	if e.Confidence < 0 || e.Confidence > 1 {
		return fmt.Errorf("confidence %v is outside [0,1]", e.Confidence)
	}
	return nil
}

// extractJSONObject tolerates the chatter and code fences a model may wrap the
// object in by taking the span between the first and last brace.
func extractJSONObject(raw []byte) []byte {
	start := bytes.IndexByte(raw, '{')
	end := bytes.LastIndexByte(raw, '}')
	if start < 0 || end < start {
		return nil
	}
	return raw[start : end+1]
}

// outputSchema describes Explanation for Ollama's structured-output mode.
func outputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary":       map[string]any{"type": "string"},
			"likely_cause":  map[string]any{"type": "string"},
			"evidence":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"suggested_fix": map[string]any{"type": "string"},
			"confidence":    map[string]any{"type": "number"},
			"severity":      map[string]any{"type": "string", "enum": []string{"low", "medium", "high", "critical"}},
		},
		"required": []string{"summary", "likely_cause", "evidence", "suggested_fix", "confidence", "severity"},
	}
}
