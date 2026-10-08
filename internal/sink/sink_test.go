package sink

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaheeranser/watcher/internal/backend"
)

var update = flag.Bool("update", false, "rewrite golden files")

func fullResult() Result {
	return Result{
		Fingerprint: "abc123",
		Kind:        "go-panic",
		Source:      "app.log",
		Count:       3,
		FirstSeen:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		LastSeen:    time.Date(2026, 1, 2, 3, 5, 5, 0, time.UTC),
		Explanation: &backend.Explanation{
			Summary:      "nil pointer dereference",
			LikelyCause:  "handler dereferenced a nil request",
			Evidence:     []string{"panic: boom", "goroutine 1 [running]:"},
			SuggestedFix: "guard the nil check",
			Confidence:   0.8,
			Severity:     "high",
		},
		Model: "qwen2.5-coder:0.5b",
	}
}

func unavailableResult() Result {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return Result{
		Fingerprint: "def456",
		Kind:        "generic-fatal",
		Source:      "/var/log/app.log",
		Count:       1,
		FirstSeen:   base,
		LastSeen:    base,
		ExplainErr:  "dial tcp 127.0.0.1:11434: connect: connection refused",
	}
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestJSONLGolden(t *testing.T) {
	var buf bytes.Buffer
	s := NewJSONL(&buf)
	if err := s.Emit(context.Background(), fullResult()); err != nil {
		t.Fatal(err)
	}
	if err := s.Emit(context.Background(), unavailableResult()); err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "results.jsonl", buf.String())
}

func TestTerminalGoldenWithoutColor(t *testing.T) {
	var buf bytes.Buffer
	s := NewTerminal(&buf, false)
	if err := s.Emit(context.Background(), fullResult()); err != nil {
		t.Fatal(err)
	}
	if err := s.Emit(context.Background(), unavailableResult()); err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "results.txt", buf.String())
}

func TestTerminalColorAddsEscapes(t *testing.T) {
	var buf bytes.Buffer
	if err := NewTerminal(&buf, true).Emit(context.Background(), fullResult()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\x1b[31m") {
		t.Errorf("expected a color escape for a high-severity result:\n%q", buf.String())
	}
}

func TestNewSelectsSinkByTerminal(t *testing.T) {
	if _, ok := New(&bytes.Buffer{}, true).(*Terminal); !ok {
		t.Error("a terminal should select the terminal sink")
	}
	if _, ok := New(&bytes.Buffer{}, false).(*JSONL); !ok {
		t.Error("a pipe should select the jsonl sink")
	}
}

func TestJSONLMarkersForUnavailable(t *testing.T) {
	var buf bytes.Buffer
	if err := NewJSONL(&buf).Emit(context.Background(), unavailableResult()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"explanation_unavailable":true`) {
		t.Errorf("missing unavailable marker: %s", out)
	}
	if !strings.Contains(out, `"evidence":[]`) {
		t.Errorf("evidence should render as an empty array, not null: %s", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("jsonl must emit exactly one line per result: %q", out)
	}
}

func TestJSONLRunID(t *testing.T) {
	t.Run("present when set", func(t *testing.T) {
		var buf bytes.Buffer
		r := fullResult()
		r.RunID = "run-014"
		if err := NewJSONL(&buf).Emit(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), `"run_id":"run-014"`) {
			t.Errorf("missing run_id: %s", buf.String())
		}
	})

	t.Run("absent when empty", func(t *testing.T) {
		var buf bytes.Buffer
		if err := NewJSONL(&buf).Emit(context.Background(), fullResult()); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(buf.String(), "run_id") {
			t.Errorf("run_id must be omitted when unset: %s", buf.String())
		}
	})
}
