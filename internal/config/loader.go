package config

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Parse resolves settings from args, the environment, and the config file, then
// validates the result. getenv is injected so tests can supply a fake
// environment.
//
// Precedence is flag > env > file > default: the config file seeds the values,
// the environment overrides only what it sets, and an explicit flag wins over
// both. An absent config file leaves the compiled-in defaults untouched
// (INST-CFG-2, INST-CFG-3).
func Parse(args []string, getenv func(string) string) (Config, error) {
	cfg := Default()

	path := configPathFromArgs(args, getenv)
	loaded, err := loadConfigFile(&cfg, path)
	if err != nil {
		return Config{}, err
	}
	cfg.ConfigPath = path
	cfg.ConfigFileLoaded = loaded

	if err := applyEnv(&cfg, getenv); err != nil {
		return Config{}, err
	}

	fs := flag.NewFlagSet("watcher", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	config := fs.String("config", path, "path to the TOML configuration file")
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
	runID := fs.String("run-id", cfg.RunID, "run identifier echoed on every result for evaluation")

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
	resolveWindow := fs.Duration("resolve-window", cfg.ResolveWindow, "quiet period before an incident is announced resolved")
	heartbeatURL := fs.String("heartbeat-url", cfg.HeartbeatURL, "dead-man's-switch URL; empty disables the heartbeat")
	heartbeatInterval := fs.Duration("heartbeat-interval", cfg.HeartbeatInterval, "interval between heartbeat pings")
	api := fs.Bool("api", cfg.APIEnabled, "serve the read API the dashboard attaches to")
	apiSocket := fs.String("api-socket", cfg.APISocket, "unix socket path for the read API")
	dbPath := fs.String("db", cfg.DBPath, "SQLite incident history file")
	retention := fs.Duration("retention", cfg.Retention, "how long incident history is kept; 0 disables the age cap")
	occurrenceCap := fs.Int("occurrence-cap", cfg.OccurrenceCap, "occurrence rows kept per incident; 0 disables the cap")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}

	cfg.ConfigPath = *config
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
	cfg.RunID = *runID
	cfg.DockerHost = *dockerHost
	cfg.DockerSince = *dockerSince
	cfg.WebhookURL = *webhookURL
	cfg.WebhookFormat = *webhookFormat
	cfg.WebhookRetries = *webhookRetries
	cfg.WebhookBackoffBase = *webhookBackoffBase
	cfg.WebhookBackoffMax = *webhookBackoffMax
	cfg.WebhookFallback = *webhookFallback
	cfg.ThrottleWindow = *throttleWindow
	cfg.ResolveWindow = *resolveWindow
	cfg.HeartbeatURL = *heartbeatURL
	cfg.HeartbeatInterval = *heartbeatInterval
	cfg.APIEnabled = *api
	cfg.APISocket = *apiSocket
	cfg.DBPath = *dbPath
	cfg.Retention = *retention
	cfg.OccurrenceCap = *occurrenceCap

	// A repeatable flag replaces the environment list outright rather than
	// appending to it, so `--source` keeps the flag > env precedence.
	if sources.set {
		cfg.Sources = sources.specs
	}
	cfg.Containers = containers.value
	cfg.ContainersSet = cfg.containersFromFile || cfg.containersFromEnv || containers.set

	if err := cfg.Validate(); err != nil {
		// A bad value merged from the file is reported with the file named, so
		// the operator knows which rung to fix (INST-CFG-4).
		if loaded {
			return Config{}, fmt.Errorf("config file %s: %w", path, err)
		}
		return Config{}, err
	}
	return cfg, nil
}

// configPathFromArgs resolves the config-file path with the usual precedence:
// an explicit --config wins, then WATCHER_CONFIG, then the platform default.
func configPathFromArgs(args []string, getenv func(string) string) string {
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(args[i], "=")
		if name != "--config" && name != "-config" {
			continue
		}
		if hasValue {
			return value
		}
		if i+1 < len(args) {
			return args[i+1]
		}
	}
	if v := getenv("WATCHER_CONFIG"); v != "" {
		return v
	}
	return DefaultConfigPath(getenv)
}

// applyEnv overlays the environment onto cfg in place. Every value is applied
// only when its variable is set, so a value from the config file survives an
// unset variable and the environment still wins over the file (INST-CFG-2).
func applyEnv(cfg *Config, getenv func(string) string) error {
	if v := getenv("WATCHER_FILE"); v != "" {
		cfg.File = v
	}
	if v := getenv("WATCHER_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := getenv("WATCHER_RUN_ID"); v != "" {
		cfg.RunID = v
	}
	if v := getenv("WATCHER_OLLAMA_URL"); v != "" {
		cfg.OllamaURL = v
	}

	if raw := getenv("WATCHER_SOURCES"); raw != "" {
		specs, err := parseSourceList(raw)
		if err != nil {
			return err
		}
		cfg.Sources = specs
	}

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
		{"WATCHER_OCCURRENCE_CAP", &cfg.OccurrenceCap},
	}
	for _, e := range ints {
		v, err := envInt(getenv, e.name, *e.dst)
		if err != nil {
			return err
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
		{"WATCHER_RESOLVE_WINDOW", &cfg.ResolveWindow},
		{"WATCHER_HEARTBEAT_INTERVAL", &cfg.HeartbeatInterval},
		{"WATCHER_RETENTION", &cfg.Retention},
	}
	for _, e := range durations {
		v, err := envDuration(getenv, e.name, *e.dst)
		if err != nil {
			return err
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
	if v := getenv("WATCHER_HEARTBEAT_URL"); v != "" {
		cfg.HeartbeatURL = v
	}
	if v := getenv("WATCHER_DB"); v != "" {
		cfg.DBPath = v
	}
	if v := getenv("WATCHER_API_SOCKET"); v != "" {
		cfg.APISocket = v
	} else if !cfg.socketFromFile {
		cfg.APISocket = defaultSocket(getenv)
	}

	fromStart, err := envBool(getenv, "WATCHER_FROM_START", cfg.FromStart)
	if err != nil {
		return err
	}
	cfg.FromStart = fromStart

	apiEnabled, err := envBool(getenv, "WATCHER_API", cfg.APIEnabled)
	if err != nil {
		return err
	}
	cfg.APIEnabled = apiEnabled
	return nil
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
