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
					c.OllamaTimeout == 60*time.Second && c.OllamaMaxTokens == 512 &&
					c.ExplainWindow == 15*time.Minute &&
					c.MaxBlockLines == 200 && !c.FromStart
			},
		},
		{
			name: "environment overrides defaults",
			args: []string{"--model", "m"},
			env: map[string]string{
				"WATCHER_OLLAMA_URL":         "http://ollama:11434",
				"WATCHER_CONTEXT_BEFORE":     "5",
				"WATCHER_WORKERS":            "4",
				"WATCHER_OLLAMA_TIMEOUT":     "5s",
				"WATCHER_OLLAMA_MAX_TOKENS":  "256",
				"WATCHER_EXPLAIN_WINDOW":     "1m",
				"WATCHER_FROM_START":         "true",
				"WATCHER_MAX_BLOCK_LINES":    "50",
				"WATCHER_RESOLVE_WINDOW":     "90s",
				"WATCHER_HEARTBEAT_URL":      "https://hc-ping.example/abc",
				"WATCHER_HEARTBEAT_INTERVAL": "30s",
			},
			want: func(c Config) bool {
				return c.OllamaURL == "http://ollama:11434" && c.ContextBefore == 5 &&
					c.Workers == 4 && c.OllamaTimeout == 5*time.Second &&
					c.OllamaMaxTokens == 256 &&
					c.ExplainWindow == time.Minute && c.FromStart && c.MaxBlockLines == 50 &&
					c.ResolveWindow == 90*time.Second &&
					c.HeartbeatURL == "https://hc-ping.example/abc" && c.HeartbeatInterval == 30*time.Second
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

func TestParseRunID(t *testing.T) {
	got, err := Parse([]string{"--model", "m"}, envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.RunID != "" {
		t.Errorf("run-id = %q, want empty by default", got.RunID)
	}

	got, err = Parse([]string{"--model", "m"}, envFrom(map[string]string{"WATCHER_RUN_ID": "run-014"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.RunID != "run-014" {
		t.Errorf("run-id = %q, want run-014 from environment", got.RunID)
	}

	got, err = Parse([]string{"--model", "m", "--run-id", "run-flag"}, envFrom(map[string]string{"WATCHER_RUN_ID": "run-014"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.RunID != "run-flag" {
		t.Errorf("run-id = %q, want the flag to win", got.RunID)
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

	_, err = Parse([]string{"--model", "m"}, envFrom(map[string]string{"WATCHER_SOURCES": "not-a-source"}))
	if err == nil || !strings.Contains(err.Error(), "WATCHER_SOURCES") {
		t.Fatalf("error = %v, want mention of WATCHER_SOURCES", err)
	}
}

func TestParseSources(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want []SourceSpec
	}{
		{
			name: "file shorthand becomes a single source",
			args: []string{"--model", "m", "--file", "/var/log/app.log"},
			want: []SourceSpec{{Path: "/var/log/app.log"}},
		},
		{
			name: "labeled sources from flags",
			args: []string{"--model", "m", "--source", "backend=/var/log/backend.log", "--source", "worker=-"},
			want: []SourceSpec{{Label: "backend", Path: "/var/log/backend.log"}, {Label: "worker", Path: "-"}},
		},
		{
			name: "sources from environment",
			env:  map[string]string{"WATCHER_SOURCES": "api=/var/log/api.log,db=-"},
			args: []string{"--model", "m"},
			want: []SourceSpec{{Label: "api", Path: "/var/log/api.log"}, {Label: "db", Path: "-"}},
		},
		{
			name: "flags replace environment sources",
			args: []string{"--model", "m", "--source", "flag=/f.log"},
			env:  map[string]string{"WATCHER_SOURCES": "env=/e.log"},
			want: []SourceSpec{{Label: "flag", Path: "/f.log"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.args, envFrom(tt.env))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !equalSources(got.ResolvedSources(), tt.want) {
				t.Errorf("sources = %+v, want %+v", got.ResolvedSources(), tt.want)
			}
		})
	}
}

func TestSourceEffectiveLabel(t *testing.T) {
	tests := []struct {
		spec SourceSpec
		want string
	}{
		{SourceSpec{Path: "/var/log/app.log"}, "/var/log/app.log"},
		{SourceSpec{Path: "-"}, "stdin"},
		{SourceSpec{Path: ""}, "stdin"},
		{SourceSpec{Label: "backend", Path: "/var/log/app.log"}, "backend"},
		{SourceSpec{Label: "worker", Path: "-"}, "worker"},
	}
	for _, tt := range tests {
		if got := tt.spec.EffectiveLabel(); got != tt.want {
			t.Errorf("%+v: label = %q, want %q", tt.spec, got, tt.want)
		}
	}
}

func TestParseRuntimeDefaults(t *testing.T) {
	got, err := Parse([]string{"--model", "m"}, envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.DockerHost != DefaultDockerHost {
		t.Errorf("docker-host = %q, want %q", got.DockerHost, DefaultDockerHost)
	}
	if got.DockerSince != 0 {
		t.Errorf("docker-since = %s, want 0s", got.DockerSince)
	}
	if got.WebhookFormat != "generic" || got.WebhookRetries != 5 ||
		got.WebhookBackoffBase != time.Second || got.WebhookBackoffMax != 30*time.Second {
		t.Errorf("unexpected webhook defaults: %+v", got)
	}
	if got.WebhookFallback != DefaultWebhookFallback {
		t.Errorf("webhook-fallback = %q, want %q", got.WebhookFallback, DefaultWebhookFallback)
	}
	if got.ThrottleWindow != 15*time.Minute {
		t.Errorf("throttle-window = %s, want 15m", got.ThrottleWindow)
	}
	if got.ResolveWindow != DefaultResolveWindow {
		t.Errorf("resolve-window = %s, want %s", got.ResolveWindow, DefaultResolveWindow)
	}
	if got.HeartbeatURL != "" || got.HeartbeatInterval != DefaultHeartbeatInterval {
		t.Errorf("heartbeat defaults = %q/%s, want disabled at %s", got.HeartbeatURL, got.HeartbeatInterval, DefaultHeartbeatInterval)
	}
	if got.ContainersSet {
		t.Error("containers should be unset by default")
	}
}

func TestParseDockerSelector(t *testing.T) {
	got, err := Parse([]string{"--model", "m", "--containers", "project=shop"}, envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.Containers != "project=shop" || !got.ContainersSet {
		t.Errorf("containers = %q set=%v, want project=shop set", got.Containers, got.ContainersSet)
	}

	got, err = Parse([]string{"--model", "m"}, envFrom(map[string]string{"WATCHER_CONTAINERS": "none"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Containers != ContainersDisabled || !got.ContainersSet {
		t.Errorf("containers = %q set=%v, want none set", got.Containers, got.ContainersSet)
	}
}

func TestParseValidationRuntime(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantSub string
	}{
		{
			name:    "duplicate labels rejected",
			args:    []string{"--model", "m", "--source", "restart=/a.log", "--source", "restart=/b.log"},
			wantSub: "duplicate source label",
		},
		{
			name:    "explicit label colliding with a default label rejected",
			args:    []string{"--model", "m", "--file", "/a.log", "--source", "/a.log=/b.log"},
			wantSub: "duplicate source label",
		},
		{
			name:    "bad webhook format",
			args:    []string{"--model", "m", "--webhook-format", "teams"},
			wantSub: "webhook-format",
		},
		{
			name:    "bad webhook url",
			args:    []string{"--model", "m", "--webhook-url", "ftp://example.com"},
			wantSub: "webhook-url",
		},
		{
			name:    "webhook retries must be positive",
			args:    []string{"--model", "m", "--webhook-retries", "0"},
			wantSub: "webhook-retries must be at least 1",
		},
		{
			name:    "backoff max below base",
			args:    []string{"--model", "m", "--webhook-backoff-base", "10s", "--webhook-backoff-max", "1s"},
			wantSub: "webhook-backoff-max",
		},
		{
			name:    "negative docker-since",
			args:    []string{"--model", "m", "--docker-since", "-1s"},
			wantSub: "docker-since must not be negative",
		},
		{
			name:    "negative throttle window",
			args:    []string{"--model", "m", "--throttle-window", "-1s"},
			wantSub: "throttle-window must not be negative",
		},
		{
			name:    "resolve window must be positive",
			args:    []string{"--model", "m", "--resolve-window", "0s"},
			wantSub: "resolve-window must be positive",
		},
		{
			name:    "heartbeat interval must be positive",
			args:    []string{"--model", "m", "--heartbeat-interval", "0s"},
			wantSub: "heartbeat-interval must be positive",
		},
		{
			name:    "bad heartbeat url",
			args:    []string{"--model", "m", "--heartbeat-url", "ftp://example.com"},
			wantSub: "heartbeat-url",
		},
		{
			name:    "source without label",
			args:    []string{"--model", "m", "--source", "/a.log"},
			wantSub: "label=path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.args, envFrom(nil))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantSub)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q, want substring %q", err, tt.wantSub)
			}
		})
	}
}

func equalSources(got, want []SourceSpec) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
