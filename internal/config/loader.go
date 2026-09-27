package config

import (
	"flag"
	"fmt"
	"io"
	"strconv"
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
	explainWindow := fs.Duration("explain-window", cfg.ExplainWindow, "minimum interval between explanations of one fingerprint")
	maxBlockLines := fs.Int("max-block-lines", cfg.MaxBlockLines, "maximum lines kept in a crash block")
	fromStart := fs.Bool("from-start", cfg.FromStart, "read a file source from the beginning instead of the end")

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
	cfg.ExplainWindow = *explainWindow
	cfg.MaxBlockLines = *maxBlockLines
	cfg.FromStart = *fromStart

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

	ints := []struct {
		name string
		dst  *int
	}{
		{"WATCHER_CONTEXT_BEFORE", &cfg.ContextBefore},
		{"WATCHER_CONTEXT_AFTER", &cfg.ContextAfter},
		{"WATCHER_CONTEXT_BUDGET", &cfg.ContextBudget},
		{"WATCHER_WORKERS", &cfg.Workers},
		{"WATCHER_MAX_BLOCK_LINES", &cfg.MaxBlockLines},
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
	}
	for _, e := range durations {
		v, err := envDuration(getenv, e.name, *e.dst)
		if err != nil {
			return Config{}, err
		}
		*e.dst = v
	}

	v, err := envBool(getenv, "WATCHER_FROM_START", cfg.FromStart)
	if err != nil {
		return Config{}, err
	}
	cfg.FromStart = v
	return cfg, nil
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
