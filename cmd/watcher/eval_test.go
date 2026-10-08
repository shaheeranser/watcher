package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const evalTruth = `{"cases":[
  {"id":"run-a","expected_cause":"nil pointer dereference in config loader","kind":"go-panic"},
  {"id":"run-b","expected_cause":"disk full writing WAL","kind":"generic-fatal"}
]}`

const evalResults = `{"fingerprint":"fp-a","kind":"go-panic","likely_cause":"nil pointer dereference in config loader","severity":"high","confidence":0.9,"run_id":"run-a"}
{"fingerprint":"fp-b","kind":"generic-fatal","likely_cause":"permission denied on /var/lib/app","severity":"high","confidence":0.7,"run_id":"run-b"}`

func TestEvalVerbIsRegistered(t *testing.T) {
	if _, ok := verbs["eval"]; !ok {
		t.Fatal("the eval verb must be registered")
	}
}

func TestEvalHelpExitsZero(t *testing.T) {
	silenceStderr(t)
	if code := evalCommand([]string{"--help"}); code != 0 {
		t.Errorf("eval --help exit = %d, want 0", code)
	}
}

func writeEvalInputs(t *testing.T, truth, results string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	truthPath := filepath.Join(dir, "truth.json")
	resultsPath := filepath.Join(dir, "results.jsonl")
	if err := os.WriteFile(truthPath, []byte(truth), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultsPath, []byte(results), 0o644); err != nil {
		t.Fatal(err)
	}
	return truthPath, resultsPath
}

func captureStdout(t *testing.T) func() string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = file
	t.Cleanup(func() { os.Stdout = original })
	return func() string {
		if _, err := file.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
}

func feedStdin(t *testing.T, content string) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	original := os.Stdin
	os.Stdin = file
	t.Cleanup(func() { os.Stdin = original })
}

func TestEvalExitCodeStraddlesGate(t *testing.T) {
	truthPath, resultsPath := writeEvalInputs(t, evalTruth, evalResults)
	tests := []struct {
		name     string
		minRate  string
		wantCode int
	}{
		{"no gate is report-only", "", 0},
		{"gate below the pass rate passes", "0.4", 0},
		{"gate above the pass rate fails", "0.6", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			silenceStderr(t)
			captureStdout(t)
			args := []string{"--truth", truthPath, "--results", resultsPath}
			if tt.minRate != "" {
				args = append(args, "--min-pass-rate", tt.minRate)
			}
			if code := evalCommand(args); code != tt.wantCode {
				t.Errorf("exit = %d, want %d", code, tt.wantCode)
			}
		})
	}
}

func TestEvalReadsResultsFromStdin(t *testing.T) {
	truthPath, _ := writeEvalInputs(t, evalTruth, evalResults)
	feedStdin(t, evalResults)
	silenceStderr(t)
	out := captureStdout(t)

	if code := evalCommand([]string{"--truth", truthPath, "--results", "-"}); code != 0 {
		t.Errorf("stdin pipeline exit = %d, want 0", code)
	}
	if !strings.Contains(out(), "PASS 1 / 2") {
		t.Errorf("stdin results were not scored:\n%s", out())
	}
}

func TestEvalJSONFormat(t *testing.T) {
	truthPath, resultsPath := writeEvalInputs(t, evalTruth, evalResults)
	silenceStderr(t)
	out := captureStdout(t)

	if code := evalCommand([]string{"--truth", truthPath, "--results", resultsPath, "--format", "json"}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := out()
	for _, want := range []string{`"schema_version": 1`, `"identifier": "run_id"`, `"verdict": "pass"`, `"cause_mismatch"`} {
		if !strings.Contains(got, want) {
			t.Errorf("JSON report missing %q:\n%s", want, got)
		}
	}
}

func TestEvalMalformedTruthExitsTwo(t *testing.T) {
	truthPath, resultsPath := writeEvalInputs(t, `{"cases":[{"id":"a","expected_cause":"x"},{"id":"a","expected_cause":"y"}]}`, evalResults)
	silenceStderr(t)
	captureStdout(t)
	if code := evalCommand([]string{"--truth", truthPath, "--results", resultsPath}); code != 2 {
		t.Errorf("duplicate-id truth exit = %d, want 2", code)
	}
}

func TestEvalEmptyResultsExitsTwo(t *testing.T) {
	truthPath, resultsPath := writeEvalInputs(t, evalTruth, "\n\n")
	silenceStderr(t)
	captureStdout(t)
	if code := evalCommand([]string{"--truth", truthPath, "--results", resultsPath}); code != 2 {
		t.Errorf("empty-results exit = %d, want 2", code)
	}
}

func TestEvalMissingTruthExitsTwo(t *testing.T) {
	silenceStderr(t)
	captureStdout(t)
	_, resultsPath := writeEvalInputs(t, evalTruth, evalResults)
	missing := filepath.Join(t.TempDir(), "absent.json")
	if code := evalCommand([]string{"--truth", missing, "--results", resultsPath}); code != 2 {
		t.Errorf("missing truth exit = %d, want 2", code)
	}
}

func TestEvalKindMismatchRecording(t *testing.T) {
	truth := `{"cases":[{"id":"run-a","expected_cause":"nil pointer dereference in config loader","kind":"go-panic"}]}`
	results := `{"fingerprint":"fp-a","kind":"generic-fatal","likely_cause":"nil pointer dereference in config loader","severity":"high","confidence":0.9,"run_id":"run-a"}`
	truthPath, resultsPath := writeEvalInputs(t, truth, results)

	// A kind mismatch is recorded but passes unless --require-kind is set.
	silenceStderr(t)
	out := captureStdout(t)
	if code := evalCommand([]string{"--truth", truthPath, "--results", resultsPath, "--format", "json", "--min-pass-rate", "1.0"}); code != 0 {
		t.Errorf("kind mismatch without require-kind exit = %d, want 0", code)
	}
	if !strings.Contains(out(), `"kind_mismatch"`) || !strings.Contains(out(), `"kind_match": false`) {
		t.Errorf("kind mismatch should be recorded:\n%s", out())
	}

	// With --require-kind it fails the gate.
	silenceStderr(t)
	captureStdout(t)
	if code := evalCommand([]string{"--truth", truthPath, "--results", resultsPath, "--require-kind", "--min-pass-rate", "1.0"}); code != 1 {
		t.Errorf("kind mismatch with require-kind exit = %d, want 1", code)
	}
}
