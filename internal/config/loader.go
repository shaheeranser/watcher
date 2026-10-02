package config

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Parse resolves settings from args and the environment, then validates the
// result. getenv is injected so tests can supply a fake environment.
//
// Precedence is flag > env > default: environment values seed the flag
// defaults, so an explicit flag always wins.
func Parse(args []string, getenv func(string) string) (Config, error) {
	cfg, err := fromEnv(getenv)
	if err != nil {
		return Config{}, err
	}

	fs := flag.NewFlagSet("watcher", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	file := fs.String("file", cfg.File, "log file to tail; empty reads stdin")
	ollamaURL := fs.String("ollama-url", cfg.OllamaURL, "base URL of the Ollama server")
	model := fs.String("model", cfg.Model, "Ollama model name (required)")
	contextBefore := fs.Int("context-before", cfg.ContextBefore, "lines of context before the crash block")
	contextAfter := fs.Int("context-after", cfg.ContextAfter, "lines of context after the crash block")
	contextBudget := fs.Int("context-budget", cfg.ContextBudget, "excerpt size budget in bytes")
	workers := fs.Int("workers", cfg.Workers, "concurrent backend workers")
	ollamaTimeout := fs.Duration("ollama-timeout", cfg.OllamaTimeout, "timeout for a single model request")
	ollamaMaxTokens := fs.Int("ollama-max-tokens", cfg.OllamaMaxTokens, "maximum tokens the model may generate per request")
	explainWindow := fs.Duration("explain-window", cfg.ExplainWindow, "minimum interval between explanations of one fingerprint")
	maxBlockLines := fs.Int("max-block-lines", cfg.MaxBlockLines, "maximum lines kept in a crash block")
	fromStart := fs.Bool("from-start", cfg.FromStart, "read a file source from the beginning instead of the end")

	sources := &sourceListValue{}
	fs.Var(sources, "source", "labeled log source, label=path (repeatable; path '-' is stdin)")
	containers := &stringValue{value: cfg.Containers}
	fs.Var(containers, "containers", "Docker selector: project=NAME, label=KEY=VALUE, name=NAME, service=NAME, or 'none' to disable")
	dockerHost := fs.String("docker-host", cfg.DockerHost, "Docker Engine host (unix:// or tcp://)")
	dockerSince := fs.Duration("docker-since", cfg.DockerSince, "how far back to read existing container logs")
	webhookURL := fs.String("webhook-url", cfg.WebhookURL, "webhook URL for notifications; empty disables the sink")
	webhookFormat := fs.String("webhook-format", cfg.WebhookFormat, "webhook payload provider: generic, slack, or discord")
	webhookRetries := fs.Int("webhook-retries", cfg.WebhookRetries, "maximum delivery attempts before the fallback file")
	webhookBackoffBase := fs.Duration("webhook-backoff-base", cfg.WebhookBackoffBase, "first retry delay")
	webhookBackoffMax := fs.Duration("webhook-backoff-max", cfg.WebhookBackoffMax, "upper bound on the retry delay")
	webhookFallback := fs.String("webhook-fallback", cfg.WebhookFallback, "file that receives undeliverable notifications")
	throttleWindow := fs.Duration("throttle-window", cfg.ThrottleWindow, "minimum interval between notifications for one (label, fingerprint)")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}

	cfg.File = *file
	cfg.OllamaURL = *ollamaURL
	cfg.Model = *model
	cfg.ContextBefore = *contextBefore
	cfg.ContextAfter = *contextAfter
	cfg.ContextBudget = *contextBudget
	cfg.Workers = *workers
	cfg.OllamaTimeout = *ollamaTimeout
	cfg.OllamaMaxTokens = *ollamaMaxTokens
	cfg.ExplainWindow = *explainWindow
	cfg.MaxBlockLines = *maxBlockLines
	cfg.FromStart = *fromStart
	cfg.DockerHost = *dockerHost
	cfg.DockerSince = *dockerSince
	cfg.WebhookURL = *webhookURL
	cfg.WebhookFormat = *webhookFormat
	cfg.WebhookRetries = *webhookRetries
	cfg.WebhookBackoffBase = *webhookBackoffBase
	cfg.WebhookBackoffMax = *webhookBackoffMax
	cfg.WebhookFallback = *webhookFallback
	cfg.ThrottleWindow = *throttleWindow

	// A repeatable flag replaces the environment list outright rather than
	// appending to it, so `--source` keeps the flag > env precedence.
	if sources.set {
		cfg.Sources = sources.specs
	}
	cfg.Containers = containers.value
	cfg.ContainersSet = cfg.containersFromEnv || containers.set

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func fromEnv(getenv func(string) string) (Config, error) {
	cfg := Default()
	cfg.File = getenv("WATCHER_FILE")
	cfg.Model = getenv("WATCHER_MODEL")
	if v := getenv("WATCHER_OLLAMA_URL"); v != "" {
		cfg.OllamaURL = v
	}

	specs, err := parseSourceList(getenv("WATCHER_SOURCES"))
	if err != nil {
		return Config{}, err
	}
	cfg.Sources = specs

	ints := []struct {
		name string
		dst  *int
	}{
		{"WATCHER_CONTEXT_BEFORE", &cfg.ContextBefore},
		{"WATCHER_CONTEXT_AFTER", &cfg.ContextAfter},
		{"WATCHER_CONTEXT_BUDGET", &cfg.ContextBudget},
		{"WATCHER_WORKERS", &cfg.Workers},
		{"WATCHER_OLLAMA_MAX_TOKENS", &cfg.OllamaMaxTokens},
		{"WATCHER_MAX_BLOCK_LINES", &cfg.MaxBlockLines},
		{"WATCHER_WEBHOOK_RETRIES", &cfg.WebhookRetries},
	}
	for _, e := range ints {
		v, err := envInt(getenv, e.name, *e.dst)
		if err != nil {
			return Config{}, err
		}
		*e.dst = v
	}

	durations := []struct {
		name string
		dst  *time.Duration
	}{
		{"WATCHER_OLLAMA_TIMEOUT", &cfg.OllamaTimeout},
		{"WATCHER_EXPLAIN_WINDOW", &cfg.ExplainWindow},
		{"WATCHER_DOCKER_SINCE", &cfg.DockerSince},
		{"WATCHER_WEBHOOK_BACKOFF_BASE", &cfg.WebhookBackoffBase},
		{"WATCHER_WEBHOOK_BACKOFF_MAX", &cfg.WebhookBackoffMax},
		{"WATCHER_THROTTLE_WINDOW", &cfg.ThrottleWindow},
	}
	for _, e := range durations {
		v, err := envDuration(getenv, e.name, *e.dst)
		if err != nil {
			return Config{}, err
		}
		*e.dst = v
	}

	if v := getenv("WATCHER_DOCKER_HOST"); v != "" {
		cfg.DockerHost = v
	}
	if v := getenv("WATCHER_CONTAINERS"); v != "" {
		cfg.Containers = v
		cfg.containersFromEnv = true
	}
	if v := getenv("WATCHER_WEBHOOK_URL"); v != "" {
		cfg.WebhookURL = v
	}
	if v := getenv("WATCHER_WEBHOOK_FORMAT"); v != "" {
		cfg.WebhookFormat = v
	}
	if v := getenv("WATCHER_WEBHOOK_FALLBACK"); v != "" {
		cfg.WebhookFallback = v
	}

	v, err := envBool(getenv, "WATCHER_FROM_START", cfg.FromStart)
	if err != nil {
		return Config{}, err
	}
	cfg.FromStart = v
	return cfg, nil
}

// sourceListValue collects repeatable --source flags. It records whether any
// were supplied so an explicit flag list can replace the environment list.
type sourceListValue struct {
	specs []SourceSpec
	set   bool
}

func (v *sourceListValue) String() string {
	parts := make([]string, len(v.specs))
	for i, s := range v.specs {
		parts[i] = s.Label + "=" + s.Path
	}
	return strings.Join(parts, ",")
}

func (v *sourceListValue) Set(raw string) error {
	spec, err := parseSource(raw)
	if err != nil {
		return err
	}
	v.specs = append(v.specs, spec)
	v.set = true
	return nil
}

// stringValue is a flag that records whether it was set, so an explicit empty
// value (e.g. --containers=) is not confused with an absent flag.
type stringValue struct {
	value string
	set   bool
}

func (v *stringValue) String() string { return v.value }

func (v *stringValue) Set(raw string) error {
	v.value = raw
	v.set = true
	return nil
}

// parseSourceList decodes WATCHER_SOURCES, a comma-separated list of
// label=path entries.
func parseSourceList(raw string) ([]SourceSpec, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	specs := make([]SourceSpec, 0, len(parts))
	for _, part := range parts {
		spec, err := parseSource(part)
		if err != nil {
			return nil, fmt.Errorf("environment WATCHER_SOURCES: %w", err)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// parseSource decodes one "label=path" source. The label is required because an
// unlabeled source already has the --file shorthand.
func parseSource(raw string) (SourceSpec, error) {
	label, path, ok := strings.Cut(raw, "=")
	if !ok {
		return SourceSpec{}, fmt.Errorf("source %q is not label=path", raw)
	}
	if label == "" {
		return SourceSpec{}, fmt.Errorf("source %q has an empty label", raw)
	}
	return SourceSpec{Label: label, Path: path}, nil
}

func envInt(getenv func(string) string, name string, def int) (int, error) {
	raw := getenv(name)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("environment %s: %w", name, err)
	}
	return n, nil
}

func envDuration(getenv func(string) string, name string, def time.Duration) (time.Duration, error) {
	raw := getenv(name)
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("environment %s: %w", name, err)
	}
	return d, nil
}

func envBool(getenv func(string) string, name string, def bool) (bool, error) {
	raw := getenv(name)
	if raw == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("environment %s: %w", name, err)
	}
	return b, nil
}
