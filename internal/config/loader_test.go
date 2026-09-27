package config

import (
	"strings"
	"testing"
	"time"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParsePrecedence(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want func(Config) bool
	}{
		{
			name: "defaults apply when nothing is set",
			args: []string{"--model", "m"},
			env:  nil,
			want: func(c Config) bool {
				return c.Model == "m" &&
					c.OllamaURL == DefaultOllamaURL &&
					c.ContextBefore == 20 && c.ContextAfter == 10 &&
					c.ContextBudget == 8192 && c.Workers == 1 &&
					c.OllamaTimeout == 60*time.Second && c.ExplainWindow == 15*time.Minute &&
					c.MaxBlockLines == 200 && !c.FromStart
			},
		},
		{
			name: "environment overrides defaults",
			args: []string{"--model", "m"},
			env: map[string]string{
				"WATCHER_OLLAMA_URL":      "http://ollama:11434",
				"WATCHER_CONTEXT_BEFORE":  "5",
				"WATCHER_WORKERS":         "4",
				"WATCHER_OLLAMA_TIMEOUT":  "5s",
				"WATCHER_EXPLAIN_WINDOW":  "1m",
				"WATCHER_FROM_START":      "true",
				"WATCHER_MAX_BLOCK_LINES": "50",
			},
			want: func(c Config) bool {
				return c.OllamaURL == "http://ollama:11434" && c.ContextBefore == 5 &&
					c.Workers == 4 && c.OllamaTimeout == 5*time.Second &&
					c.ExplainWindow == time.Minute && c.FromStart && c.MaxBlockLines == 50
			},
		},
		{
			name: "flag overrides environment",
			args: []string{"--model", "flag-model", "--workers", "7", "--explain-window", "2m"},
			env:  map[string]string{"WATCHER_MODEL": "env-model", "WATCHER_WORKERS": "3"},
			want: func(c Config) bool {
				return c.Model == "flag-model" && c.Workers == 7 && c.ExplainWindow == 2*time.Minute
			},
		},
		{
			name: "file from env",
			args: []string{"--model", "m"},
			env:  map[string]string{"WATCHER_FILE": "/var/log/app.log"},
			want: func(c Config) bool { return c.File == "/var/log/app.log" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.args, envFrom(tt.env))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !tt.want(got) {
				t.Errorf("unexpected config: %+v", got)
			}
		})
	}
}

func TestParseValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantSub string
	}{
		{
			name:    "model is required",
			args:    nil,
			wantSub: "no model configured",
		},
		{
			name:    "workers must be positive",
			args:    []string{"--model", "m", "--workers", "0"},
			wantSub: "workers must be at least 1",
		},
		{
			name:    "context budget must be positive",
			args:    []string{"--model", "m", "--context-budget", "0"},
			wantSub: "context-budget must be positive",
		},
		{
			name:    "negative context before",
			args:    []string{"--model", "m", "--context-before", "-1"},
			wantSub: "context-before must not be negative",
		},
		{
			name:    "too-small block cap",
			args:    []string{"--model", "m", "--max-block-lines", "2"},
			wantSub: "max-block-lines must be at least",
		},
		{
			name:    "timeout must be positive",
			args:    []string{"--model", "m", "--ollama-timeout", "0s"},
			wantSub: "ollama-timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.args, envFrom(tt.env))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantSub)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q, want substring %q", err, tt.wantSub)
			}
		})
	}
}

func TestParseReportsAllValidationProblems(t *testing.T) {
	_, err := Parse([]string{"--workers", "0", "--context-budget", "0"}, envFrom(nil))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"no model configured", "workers must be at least 1", "context-budget must be positive"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestParseRejectsBadEnvironment(t *testing.T) {
	_, err := Parse([]string{"--model", "m"}, envFrom(map[string]string{"WATCHER_WORKERS": "lots"}))
	if err == nil || !strings.Contains(err.Error(), "WATCHER_WORKERS") {
		t.Fatalf("error = %v, want mention of WATCHER_WORKERS", err)
	}

	_, err = Parse([]string{"--model", "m"}, envFrom(map[string]string{"WATCHER_OLLAMA_TIMEOUT": "soon"}))
	if err == nil || !strings.Contains(err.Error(), "WATCHER_OLLAMA_TIMEOUT") {
		t.Fatalf("error = %v, want mention of WATCHER_OLLAMA_TIMEOUT", err)
	}
}
