package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultConfigPath resolves the config-file location: $XDG_CONFIG_HOME, else
// $HOME/.config, under the watcher directory. getenv is injected so tests can
// supply a fake environment. It returns "" when neither variable is set, which
// callers read as "no config file" (INST-CFG-1).
func DefaultConfigPath(getenv func(string) string) string {
	base := getenv("XDG_CONFIG_HOME")
	if base == "" {
		home := getenv("HOME")
		if home == "" {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, DefaultConfigDirName, ConfigFileName)
}

// tomlDuration decodes a TOML string such as "15m" through time.ParseDuration,
// so a bad duration in the file is reported with its key and line.
type tomlDuration time.Duration

func (d *tomlDuration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = tomlDuration(v)
	return nil
}

// fileConfig mirrors the config schema's shape. Its zero value is unused; a
// key contributes only when the decoder saw it, which Metadata reports.
type fileConfig struct {
	File            string       `toml:"file"`
	Model           string       `toml:"model"`
	OllamaURL       string       `toml:"ollama_url"`
	OllamaTimeout   tomlDuration `toml:"ollama_timeout"`
	OllamaMaxTokens int          `toml:"ollama_max_tokens"`
	ContextBefore   int          `toml:"context_before"`
	ContextAfter    int          `toml:"context_after"`
	ContextBudget   int          `toml:"context_budget"`
	Workers         int          `toml:"workers"`
	ExplainWindow   tomlDuration `toml:"explain_window"`
	MaxBlockLines   int          `toml:"max_block_lines"`
	FromStart       bool         `toml:"from_start"`
	RunID           string       `toml:"run_id"`

	Sources   []sourceFile  `toml:"sources"`
	Docker    dockerFile    `toml:"docker"`
	Webhook   webhookFile   `toml:"webhook"`
	Incident  incidentFile  `toml:"incident"`
	Heartbeat heartbeatFile `toml:"heartbeat"`
	API       apiFile       `toml:"api"`
}

type sourceFile struct {
	Label string `toml:"label"`
	Path  string `toml:"path"`
}

type dockerFile struct {
	Host       string       `toml:"host"`
	Since      tomlDuration `toml:"since"`
	Containers string       `toml:"containers"`
}

type webhookFile struct {
	URL         string       `toml:"url"`
	Format      string       `toml:"format"`
	Retries     int          `toml:"retries"`
	BackoffBase tomlDuration `toml:"backoff_base"`
	BackoffMax  tomlDuration `toml:"backoff_max"`
	Fallback    string       `toml:"fallback"`
}

type incidentFile struct {
	ThrottleWindow tomlDuration `toml:"throttle_window"`
	ResolveWindow  tomlDuration `toml:"resolve_window"`
}

type heartbeatFile struct {
	URL      string       `toml:"url"`
	Interval tomlDuration `toml:"interval"`
}

type apiFile struct {
	Enabled       bool         `toml:"enabled"`
	Socket        string       `toml:"socket"`
	DB            string       `toml:"db"`
	Retention     tomlDuration `toml:"retention"`
	OccurrenceCap int          `toml:"occurrence_cap"`
}

// loadConfigFile reads and decodes the file at path, applies the keys it
// defines onto cfg, and reports whether a file was loaded. An absent file is
// not an error: the caller keeps defaults (INST-CFG-3). A malformed file, an
// unknown key, or an unparseable value names the file and the problem
// (INST-CFG-4).
func loadConfigFile(cfg *Config, path string) (bool, error) {
	if path == "" {
		return false, nil
	}
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("config file %s: %w", path, err)
	}

	var fc fileConfig
	md, err := toml.DecodeFile(path, &fc)
	if err != nil {
		return false, fmt.Errorf("config file %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return false, fmt.Errorf("config file %s: unknown key(s): %s", path, keyList(undecoded))
	}

	applyFileConfig(cfg, fc, md)
	return true, nil
}

func keyList(keys []toml.Key) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = strings.Join(k, ".")
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// applyFileConfig copies each key the file defined onto cfg, leaving everything
// else at its current (default) value so the env and flag rungs can override.
func applyFileConfig(cfg *Config, fc fileConfig, md toml.MetaData) {
	if md.IsDefined("file") {
		cfg.File = fc.File
	}
	if md.IsDefined("model") {
		cfg.Model = fc.Model
	}
	if md.IsDefined("ollama_url") {
		cfg.OllamaURL = fc.OllamaURL
	}
	if md.IsDefined("ollama_timeout") {
		cfg.OllamaTimeout = time.Duration(fc.OllamaTimeout)
	}
	if md.IsDefined("ollama_max_tokens") {
		cfg.OllamaMaxTokens = fc.OllamaMaxTokens
	}
	if md.IsDefined("context_before") {
		cfg.ContextBefore = fc.ContextBefore
	}
	if md.IsDefined("context_after") {
		cfg.ContextAfter = fc.ContextAfter
	}
	if md.IsDefined("context_budget") {
		cfg.ContextBudget = fc.ContextBudget
	}
	if md.IsDefined("workers") {
		cfg.Workers = fc.Workers
	}
	if md.IsDefined("explain_window") {
		cfg.ExplainWindow = time.Duration(fc.ExplainWindow)
	}
	if md.IsDefined("max_block_lines") {
		cfg.MaxBlockLines = fc.MaxBlockLines
	}
	if md.IsDefined("from_start") {
		cfg.FromStart = fc.FromStart
	}
	if md.IsDefined("run_id") {
		cfg.RunID = fc.RunID
	}
	if md.IsDefined("sources") {
		cfg.Sources = make([]SourceSpec, len(fc.Sources))
		for i, s := range fc.Sources {
			cfg.Sources[i] = SourceSpec{Label: s.Label, Path: s.Path}
		}
	}

	if md.IsDefined("docker", "host") {
		cfg.DockerHost = fc.Docker.Host
	}
	if md.IsDefined("docker", "since") {
		cfg.DockerSince = time.Duration(fc.Docker.Since)
	}
	if md.IsDefined("docker", "containers") {
		cfg.Containers = fc.Docker.Containers
		cfg.containersFromFile = true
	}

	if md.IsDefined("webhook", "url") {
		cfg.WebhookURL = fc.Webhook.URL
	}
	if md.IsDefined("webhook", "format") {
		cfg.WebhookFormat = fc.Webhook.Format
	}
	if md.IsDefined("webhook", "retries") {
		cfg.WebhookRetries = fc.Webhook.Retries
	}
	if md.IsDefined("webhook", "backoff_base") {
		cfg.WebhookBackoffBase = time.Duration(fc.Webhook.BackoffBase)
	}
	if md.IsDefined("webhook", "backoff_max") {
		cfg.WebhookBackoffMax = time.Duration(fc.Webhook.BackoffMax)
	}
	if md.IsDefined("webhook", "fallback") {
		cfg.WebhookFallback = fc.Webhook.Fallback
	}

	if md.IsDefined("incident", "throttle_window") {
		cfg.ThrottleWindow = time.Duration(fc.Incident.ThrottleWindow)
	}
	if md.IsDefined("incident", "resolve_window") {
		cfg.ResolveWindow = time.Duration(fc.Incident.ResolveWindow)
	}

	if md.IsDefined("heartbeat", "url") {
		cfg.HeartbeatURL = fc.Heartbeat.URL
	}
	if md.IsDefined("heartbeat", "interval") {
		cfg.HeartbeatInterval = time.Duration(fc.Heartbeat.Interval)
	}

	if md.IsDefined("api", "enabled") {
		cfg.APIEnabled = fc.API.Enabled
	}
	if md.IsDefined("api", "socket") {
		cfg.APISocket = fc.API.Socket
		cfg.socketFromFile = true
	}
	if md.IsDefined("api", "db") {
		cfg.DBPath = fc.API.DB
	}
	if md.IsDefined("api", "retention") {
		cfg.Retention = time.Duration(fc.API.Retention)
	}
	if md.IsDefined("api", "occurrence_cap") {
		cfg.OccurrenceCap = fc.API.OccurrenceCap
	}
}
