// Package config defines Watcher's runtime settings, their defaults, and the
// validation applied before anything else starts. Values reach Config through
// Parse, which resolves them with flag > environment > default precedence.
package config

import (
	"errors"
	"fmt"
	"time"
)

// DefaultOllamaURL points at an Ollama instance on the local host. A container
// deployment overrides it with the service-network address instead.
const DefaultOllamaURL = "http://localhost:11434"

// minBlockLines is the smallest useful block cap: the trigger, the root line,
// and an elision marker need room even after truncation.
const minBlockLines = 4

// Config is the resolved set of runtime settings. Build it with Default or
// Parse; the zero value fails validation because it names no model.
type Config struct {
	// File is the log file to tail. Empty selects stdin.
	File string

	OllamaURL string

	// Model is required; Watcher never assumes one, so choosing a model stays
	// the deployment's decision rather than a baked-in vendor guess.
	Model string

	ContextBefore int
	ContextAfter  int

	// ContextBudget caps the excerpt in bytes.
	ContextBudget int

	Workers       int
	OllamaTimeout time.Duration
	ExplainWindow time.Duration
	MaxBlockLines int

	// FromStart makes a file source read existing content instead of starting
	// at the current end.
	FromStart bool
}

// Default returns the compiled-in settings. Model is intentionally empty; see
// Validate.
func Default() Config {
	return Config{
		OllamaURL:     DefaultOllamaURL,
		ContextBefore: 20,
		ContextAfter:  10,
		ContextBudget: 8192,
		Workers:       1,
		OllamaTimeout: 60 * time.Second,
		ExplainWindow: 15 * time.Minute,
		MaxBlockLines: 200,
		FromStart:     false,
	}
}

// Validate reports every problem at once so a misconfigured deployment learns
// about all of them in a single startup attempt.
func (c Config) Validate() error {
	var errs []error
	if c.Model == "" {
		errs = append(errs, errors.New("no model configured: set --model or WATCHER_MODEL"))
	}
	if c.OllamaURL == "" {
		errs = append(errs, errors.New("ollama URL must not be empty"))
	}
	if c.ContextBefore < 0 {
		errs = append(errs, fmt.Errorf("context-before must not be negative, got %d", c.ContextBefore))
	}
	if c.ContextAfter < 0 {
		errs = append(errs, fmt.Errorf("context-after must not be negative, got %d", c.ContextAfter))
	}
	if c.ContextBudget <= 0 {
		errs = append(errs, fmt.Errorf("context-budget must be positive, got %d", c.ContextBudget))
	}
	if c.Workers < 1 {
		errs = append(errs, fmt.Errorf("workers must be at least 1, got %d", c.Workers))
	}
	if c.OllamaTimeout <= 0 {
		errs = append(errs, fmt.Errorf("ollama-timeout must be positive, got %s", c.OllamaTimeout))
	}
	if c.ExplainWindow < 0 {
		errs = append(errs, fmt.Errorf("explain-window must not be negative, got %s", c.ExplainWindow))
	}
	if c.MaxBlockLines < minBlockLines {
		errs = append(errs, fmt.Errorf("max-block-lines must be at least %d, got %d", minBlockLines, c.MaxBlockLines))
	}
	return errors.Join(errs...)
}
