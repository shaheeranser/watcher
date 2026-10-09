package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ConfigFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultConfigPath(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "XDG config home wins",
			env:  map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/home/u"},
			want: "/xdg/watcher/config.toml",
		},
		{
			name: "HOME fallback",
			env:  map[string]string{"HOME": "/home/u"},
			want: "/home/u/.config/watcher/config.toml",
		},
		{
			name: "neither set means no file",
			env:  nil,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultConfigPath(envFrom(tt.env)); got != tt.want {
				t.Errorf("DefaultConfigPath = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigPathPrecedence(t *testing.T) {
	env := map[string]string{
		"XDG_CONFIG_HOME": "/xdg",
		"WATCHER_CONFIG":  "/env/config.toml",
	}
	if got := configPathFromArgs(nil, envFrom(env)); got != "/env/config.toml" {
		t.Errorf("path = %q, want the environment value", got)
	}
	for _, args := range [][]string{
		{"--config", "/flag/config.toml"},
		{"--config=/flag/config.toml"},
		{"-config", "/flag/config.toml"},
	} {
		if got := configPathFromArgs(args, envFrom(env)); got != "/flag/config.toml" {
			t.Errorf("args %v: path = %q, want the flag value", args, got)
		}
	}
	if got := configPathFromArgs(nil, envFrom(map[string]string{"XDG_CONFIG_HOME": "/xdg"})); got != "/xdg/watcher/config.toml" {
		t.Errorf("path = %q, want the XDG default", got)
	}
}

func TestAbsentConfigFileKeepsDefaults(t *testing.T) {
	cfg, err := Parse([]string{"--model", "m", "--config", filepath.Join(t.TempDir(), "missing.toml")}, envFrom(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.ConfigFileLoaded {
		t.Error("ConfigFileLoaded = true, want false for a missing file")
	}
	if cfg.Workers != 1 || cfg.OllamaURL != DefaultOllamaURL {
		t.Errorf("defaults were not preserved: %+v", cfg)
	}
}

func TestConfigFileLoadsValues(t *testing.T) {
	path := writeConfig(t, `
model = "from-file"
ollama_url = "http://ollama:11434"
ollama_timeout = "5s"
workers = 3
from_start = true

[docker]
containers = "project=shop"
since = "90s"

[webhook]
url = "https://example.com/hook"
format = "discord"
retries = 2

[incident]
throttle_window = "1m"
resolve_window = "30s"

[heartbeat]
url = "https://hb.example/ping"
interval = "10s"

[api]
enabled = false
socket = "/run/user/1000/watcher.sock"
retention = "1h"
`)
	cfg, err := Parse([]string{"--config", path}, envFrom(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.ConfigFileLoaded {
		t.Fatal("ConfigFileLoaded = false, want true")
	}
	if cfg.Model != "from-file" || cfg.OllamaURL != "http://ollama:11434" ||
		cfg.OllamaTimeout != 5*time.Second || cfg.Workers != 3 || !cfg.FromStart {
		t.Errorf("top-level values not applied: %+v", cfg)
	}
	if cfg.Containers != "project=shop" || !cfg.ContainersSet || cfg.DockerSince != 90*time.Second {
		t.Errorf("docker values not applied: %+v", cfg)
	}
	if cfg.WebhookURL != "https://example.com/hook" || cfg.WebhookFormat != "discord" || cfg.WebhookRetries != 2 {
		t.Errorf("webhook values not applied: %+v", cfg)
	}
	if cfg.ThrottleWindow != time.Minute || cfg.ResolveWindow != 30*time.Second {
		t.Errorf("incident values not applied: %+v", cfg)
	}
	if cfg.HeartbeatURL != "https://hb.example/ping" || cfg.HeartbeatInterval != 10*time.Second {
		t.Errorf("heartbeat values not applied: %+v", cfg)
	}
	if cfg.APIEnabled || cfg.APISocket != "/run/user/1000/watcher.sock" || cfg.Retention != time.Hour {
		t.Errorf("api values not applied: %+v", cfg)
	}
}

func TestConfigFileSocketSurvivesDefaulting(t *testing.T) {
	path := writeConfig(t, "api.socket = \"/custom/watcher.sock\"\n")
	cfg, err := Parse([]string{"--model", "m", "--config", path}, envFrom(map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.APISocket != "/custom/watcher.sock" {
		t.Errorf("api socket = %q, want the file value to survive", cfg.APISocket)
	}

	// The environment still wins over the file.
	cfg, err = Parse([]string{"--model", "m", "--config", path}, envFrom(map[string]string{"WATCHER_API_SOCKET": "/env/watcher.sock"}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.APISocket != "/env/watcher.sock" {
		t.Errorf("api socket = %q, want the environment to win", cfg.APISocket)
	}
}

func TestConfigFileMalformedNamesFile(t *testing.T) {
	path := writeConfig(t, "model = \nthis is not toml")
	_, err := Parse([]string{"--config", path}, envFrom(nil))
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("error = %v, want it to name %s", err, path)
	}
}

func TestConfigFileUnknownKeyNamesFileAndKey(t *testing.T) {
	path := writeConfig(t, "model = \"m\"\nno_such_key = 1\n")
	_, err := Parse([]string{"--config", path}, envFrom(nil))
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "no_such_key") {
		t.Fatalf("error = %v, want it to name %s and no_such_key", err, path)
	}
}

func TestConfigFileInvalidValueNamesFile(t *testing.T) {
	path := writeConfig(t, "model = \"m\"\nworkers = 0\n")
	_, err := Parse([]string{"--config", path}, envFrom(nil))
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "workers") {
		t.Fatalf("error = %v, want it to name %s and the workers problem", err, path)
	}
}

func TestConfigFileBadDurationNamesFile(t *testing.T) {
	path := writeConfig(t, "model = \"m\"\nollama_timeout = \"soon\"\n")
	_, err := Parse([]string{"--config", path}, envFrom(nil))
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("error = %v, want it to name %s", err, path)
	}
}

func TestPrecedenceAcrossAllFourRungs(t *testing.T) {
	path := writeConfig(t, `
model = "file-model"
workers = 2
ollama_url = "http://file:11434"
`)
	getenv := envFrom(map[string]string{
		"WATCHER_WORKERS":    "3",
		"WATCHER_OLLAMA_URL": "http://env:11434",
	})

	// file > default
	cfg, err := Parse([]string{"--config", path}, envFrom(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Workers != 2 || cfg.OllamaURL != "http://file:11434" || cfg.Model != "file-model" {
		t.Errorf("file rung not applied: %+v", cfg)
	}

	// env > file
	cfg, err = Parse([]string{"--config", path}, getenv)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Workers != 3 || cfg.OllamaURL != "http://env:11434" {
		t.Errorf("env rung did not win over the file: %+v", cfg)
	}

	// flag > env > file
	cfg, err = Parse([]string{"--config", path, "--workers", "4", "--ollama-url", "http://flag:11434"}, getenv)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Workers != 4 || cfg.OllamaURL != "http://flag:11434" {
		t.Errorf("flag rung did not win: %+v", cfg)
	}

	// default when no file
	cfg, err = Parse([]string{"--model", "m", "--config", filepath.Join(t.TempDir(), "none.toml")}, envFrom(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Workers != 1 || cfg.OllamaURL != DefaultOllamaURL {
		t.Errorf("defaults changed: %+v", cfg)
	}
}

func TestConfigFileSourcesArrayOfTables(t *testing.T) {
	path := writeConfig(t, `
model = "m"
[[sources]]
label = "backend"
path = "/var/log/backend.log"
[[sources]]
label = "worker"
path = "-"
`)
	cfg, err := Parse([]string{"--config", path}, envFrom(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []SourceSpec{{Label: "backend", Path: "/var/log/backend.log"}, {Label: "worker", Path: "-"}}
	if !equalSources(cfg.Sources, want) {
		t.Errorf("sources = %+v, want %+v", cfg.Sources, want)
	}

	// The environment still overrides file sources.
	cfg, err = Parse([]string{"--config", path}, envFrom(map[string]string{"WATCHER_SOURCES": "env=/e.log"}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !equalSources(cfg.Sources, []SourceSpec{{Label: "env", Path: "/e.log"}}) {
		t.Errorf("sources = %+v, want the environment list", cfg.Sources)
	}
}

func TestConfigFileRunID(t *testing.T) {
	path := writeConfig(t, "model = \"m\"\nrun_id = \"file-run\"\n")
	cfg, err := Parse([]string{"--config", path}, envFrom(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.RunID != "file-run" {
		t.Errorf("run_id = %q, want file-run", cfg.RunID)
	}
}
