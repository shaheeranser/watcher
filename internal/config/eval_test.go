package config

import (
	"strings"
	"testing"
)

func TestParseEvalDefaults(t *testing.T) {
	cfg, err := ParseEval([]string{"--truth", "t.json", "--results", "r.jsonl"}, envFrom(nil))
	if err != nil {
		t.Fatalf("ParseEval: %v", err)
	}
	if cfg.Identifier != "run_id" {
		t.Errorf("identifier = %q, want run_id", cfg.Identifier)
	}
	if cfg.Threshold != DefaultEvalThreshold {
		t.Errorf("threshold = %v, want %v", cfg.Threshold, DefaultEvalThreshold)
	}
	if cfg.Format != "text" {
		t.Errorf("format = %q, want text", cfg.Format)
	}
	if cfg.MinPassRateSet {
		t.Error("min-pass-rate should be unset by default")
	}
	if cfg.RequireKind {
		t.Error("require-kind should be off by default")
	}
}

func TestParseEvalPrecedence(t *testing.T) {
	env := map[string]string{
		"WATCHER_EVAL_TRUTH":         "env.json",
		"WATCHER_EVAL_RESULTS":       "env.jsonl",
		"WATCHER_EVAL_IDENTIFIER":    "fingerprint",
		"WATCHER_EVAL_THRESHOLD":     "0.3",
		"WATCHER_EVAL_MIN_PASS_RATE": "0.5",
		"WATCHER_EVAL_FORMAT":        "json",
	}
	cfg, err := ParseEval(nil, envFrom(env))
	if err != nil {
		t.Fatalf("ParseEval: %v", err)
	}
	if cfg.TruthPath != "env.json" || cfg.ResultsPath != "env.jsonl" {
		t.Errorf("paths = %q/%q, want the environment values", cfg.TruthPath, cfg.ResultsPath)
	}
	if cfg.Identifier != "fingerprint" || cfg.Threshold != 0.3 || cfg.Format != "json" {
		t.Errorf("unexpected env config: %+v", cfg)
	}
	if !cfg.MinPassRateSet || cfg.MinPassRate != 0.5 {
		t.Errorf("min-pass-rate = %v set=%v, want 0.5 set", cfg.MinPassRate, cfg.MinPassRateSet)
	}

	cfg, err = ParseEval([]string{
		"--truth", "flag.json", "--results", "flag.jsonl",
		"--identifier", "run_id", "--threshold", "0.9",
		"--min-pass-rate", "0.8", "--format", "both",
	}, envFrom(env))
	if err != nil {
		t.Fatalf("ParseEval: %v", err)
	}
	if cfg.TruthPath != "flag.json" || cfg.Identifier != "run_id" ||
		cfg.Threshold != 0.9 || cfg.Format != "both" || cfg.MinPassRate != 0.8 {
		t.Errorf("flags should win over the environment: %+v", cfg)
	}
}

func TestParseEvalValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantSub string
	}{
		{"truth required", []string{"--results", "r.jsonl"}, "set --truth"},
		{"results required", []string{"--truth", "t.json"}, "set --results"},
		{"identifier enum", []string{"--truth", "t", "--results", "r", "--identifier", "name"}, "identifier"},
		{"format enum", []string{"--truth", "t", "--results", "r", "--format", "yaml"}, "format"},
		{"truth-format enum", []string{"--truth", "t", "--results", "r", "--truth-format", "xml"}, "truth-format"},
		{"threshold range", []string{"--truth", "t", "--results", "r", "--threshold", "1.5"}, "threshold"},
		{"min-pass-rate range", []string{"--truth", "t", "--results", "r", "--min-pass-rate", "2"}, "min-pass-rate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseEval(tt.args, envFrom(nil))
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantSub)
			}
		})
	}
}

func TestParseEvalRejectsBadEnvironment(t *testing.T) {
	_, err := ParseEval([]string{"--truth", "t", "--results", "r"}, envFrom(map[string]string{"WATCHER_EVAL_THRESHOLD": "hot"}))
	if err == nil || !strings.Contains(err.Error(), "WATCHER_EVAL_THRESHOLD") {
		t.Fatalf("error = %v, want mention of WATCHER_EVAL_THRESHOLD", err)
	}
}
