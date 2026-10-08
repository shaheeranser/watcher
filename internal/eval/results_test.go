package eval

import (
	"errors"
	"strings"
	"testing"
)

func TestReadResultsParsesIncidents(t *testing.T) {
	stream := `
{"fingerprint":"abc","kind":"go-panic","likely_cause":"nil deref","severity":"high","confidence":0.8,"first_seen":"2026-01-02T03:04:05Z"}
{"fingerprint":"def","kind":"generic-fatal","run_id":"run-014","explanation_unavailable":true,"error":"ollama: no response"}

`
	got, err := ReadResults(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("ReadResults: %v", err)
	}
	if len(got.Incidents) != 2 {
		t.Fatalf("incidents = %d, want 2", len(got.Incidents))
	}
	first := got.Incidents[0]
	if first.Fingerprint != "abc" || first.Kind != "go-panic" || first.LikelyCause != "nil deref" ||
		first.Severity != "high" || first.Confidence != 0.8 {
		t.Errorf("first incident = %+v", first)
	}
	second := got.Incidents[1]
	if second.RunID != "run-014" || !second.ExplanationUnavailable || second.Error != "ollama: no response" {
		t.Errorf("second incident = %+v", second)
	}
	if got.Junk != 0 {
		t.Errorf("junk = %d, want 0", got.Junk)
	}
}

func TestReadResultsCountsJunkAndIgnoresNonIncidents(t *testing.T) {
	stream := strings.Join([]string{
		`not json at all`,
		`{"hello":"world"}`,
		`{"fingerprint":"abc","likely_cause":"x"}`,
		`[1,2,3]`,
		`{"another":{"nested":true}}`,
	}, "\n")
	got, err := ReadResults(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("ReadResults: %v", err)
	}
	if len(got.Incidents) != 1 {
		t.Fatalf("incidents = %d, want 1: %+v", len(got.Incidents), got.Incidents)
	}
	if got.Junk != 2 {
		t.Errorf("junk = %d, want 2 (the two unparseable lines)", got.Junk)
	}
}

func TestReadResultsToleratesUnknownFields(t *testing.T) {
	stream := `{"fingerprint":"abc","likely_cause":"x","brand_new_field":{"a":1},"severity":"low"}`
	got, err := ReadResults(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("ReadResults: %v", err)
	}
	if len(got.Incidents) != 1 || got.Incidents[0].Severity != "low" {
		t.Fatalf("unknown fields should be ignored: %+v", got.Incidents)
	}
}

func TestReadResultsEmptyInput(t *testing.T) {
	tests := []struct {
		name   string
		stream string
	}{
		{"empty", ""},
		{"blank lines only", "\n\n   \n"},
		{"only junk", "garbage\nmore garbage"},
		{"only non-incidents", `{"a":1}` + "\n" + `{"b":2}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadResults(strings.NewReader(tt.stream))
			if !errors.Is(err, ErrNoIncidents) {
				t.Fatalf("error = %v, want ErrNoIncidents", err)
			}
		})
	}
}

func TestReadResultsLongLine(t *testing.T) {
	big := strings.Repeat("x", 200_000)
	stream := `{"fingerprint":"abc","likely_cause":"` + big + `"}`
	got, err := ReadResults(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("ReadResults: %v", err)
	}
	if len(got.Incidents) != 1 || len(got.Incidents[0].LikelyCause) != len(big) {
		t.Fatalf("a long evidence/cause line must not be truncated: %d", len(got.Incidents[0].LikelyCause))
	}
}
