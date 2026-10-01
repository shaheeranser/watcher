// Package config defines Watcher's runtime settings, their defaults, and the
// validation applied before anything else starts. Values reach Config through
// Parse, which resolves them with flag > environment > default precedence.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

// DefaultOllamaURL points at an Ollama instance on the local host. A container
// deployment overrides it with the service-network address instead.
const DefaultOllamaURL = "http://localhost:11434"

// DefaultDockerHost is the local Docker Engine socket. A container deployment
// mounts the host socket at this path.
const DefaultDockerHost = "unix:///var/run/docker.sock"

// DefaultWebhookFallback is where a notification that could not be delivered is
// spooled as one JSON line, so a wedged receiver never loses the report.
const DefaultWebhookFallback = "undelivered.jsonl"

// StdinPath is the pseudo-path that names standard input in a source spec, so a
// file source and stdin can be configured side by side.
const StdinPath = "-"

// ContainersDisabled is the --containers value that turns the Docker source off
// even when the environment would otherwise default it on.
const ContainersDisabled = "none"

// minBlockLines is the smallest useful block cap: the trigger, the root line,
// and an elision marker need room even after truncation.
const minBlockLines = 4

// SourceSpec is one log source and the operator-assigned label that will
// attribute its lines. An empty label falls back to the path, or to "stdin".
type SourceSpec struct {
	Label string
	Path  string
}

// IsStdin reports whether the spec names standard input rather than a file.
func (s SourceSpec) IsStdin() bool {
	return s.Path == "" || s.Path == StdinPath
}

// EffectiveLabel is the label the source will carry once defaults are applied.
func (s SourceSpec) EffectiveLabel() string {
	if s.Label != "" {
		return s.Label
	}
	if s.IsStdin() {
		return "stdin"
	}
	return s.Path
}

// Config is the resolved set of runtime settings. Build it with Default or
// Parse; the zero value fails validation because it names no model.
type Config struct {
	// File is the shorthand single-file source. Empty selects stdin unless
	// Sources supplies one or more labeled sources instead.
	File string

	// Sources are the repeatable --source entries, each a labeled file or
	// stdin. The File shorthand is folded in by ResolvedSources.
	Sources []SourceSpec

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

	// Containers is the Docker source selector. Empty means the default scope
	// (Watcher's own Compose project); ContainersDisabled turns it off.
	Containers string

	// ContainersSet records whether the selector was supplied at all, so an
	// explicit empty value is distinguishable from an unset one.
	ContainersSet bool

	// containersFromEnv notes that WATCHER_CONTAINERS supplied the selector, so
	// Parse can fold that into ContainersSet after flag resolution.
	containersFromEnv bool

	DockerHost  string
	DockerSince time.Duration

	WebhookURL         string
	WebhookFormat      string
	WebhookRetries     int
	WebhookBackoffBase time.Duration
	WebhookBackoffMax  time.Duration
	WebhookFallback    string

	// ThrottleWindow caps how often one (label, fingerprint) may notify.
	ThrottleWindow time.Duration
}

// Default returns the compiled-in settings. Model is intentionally empty; see
// Validate.
func Default() Config {
	return Config{
		OllamaURL:          DefaultOllamaURL,
		ContextBefore:      20,
		ContextAfter:       10,
		ContextBudget:      8192,
		Workers:            1,
		OllamaTimeout:      60 * time.Second,
		ExplainWindow:      15 * time.Minute,
		MaxBlockLines:      200,
		FromStart:          false,
		DockerHost:         DefaultDockerHost,
		DockerSince:        0,
		WebhookFormat:      "generic",
		WebhookRetries:     5,
		WebhookBackoffBase: time.Second,
		WebhookBackoffMax:  30 * time.Second,
		WebhookFallback:    DefaultWebhookFallback,
		ThrottleWindow:     15 * time.Minute,
	}
}

// ResolvedSources folds the --file shorthand into the labeled source list, so
// callers see one ordered set of things to watch.
func (c Config) ResolvedSources() []SourceSpec {
	out := make([]SourceSpec, 0, len(c.Sources)+1)
	if c.File != "" {
		out = append(out, SourceSpec{Path: c.File})
	}
	out = append(out, c.Sources...)
	return out
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
	if c.DockerSince < 0 {
		errs = append(errs, fmt.Errorf("docker-since must not be negative, got %s", c.DockerSince))
	}
	if c.DockerHost == "" {
		errs = append(errs, errors.New("docker-host must not be empty"))
	}
	if c.ThrottleWindow < 0 {
		errs = append(errs, fmt.Errorf("throttle-window must not be negative, got %s", c.ThrottleWindow))
	}
	errs = append(errs, c.validateWebhook()...)
	errs = append(errs, c.validateSources()...)
	return errors.Join(errs...)
}

func (c Config) validateWebhook() []error {
	var errs []error
	switch c.WebhookFormat {
	case "generic", "slack", "discord":
	default:
		errs = append(errs, fmt.Errorf("webhook-format %q is not one of generic, slack, discord", c.WebhookFormat))
	}
	if c.WebhookRetries < 1 {
		errs = append(errs, fmt.Errorf("webhook-retries must be at least 1, got %d", c.WebhookRetries))
	}
	if c.WebhookBackoffBase <= 0 {
		errs = append(errs, fmt.Errorf("webhook-backoff-base must be positive, got %s", c.WebhookBackoffBase))
	}
	if c.WebhookBackoffMax < c.WebhookBackoffBase {
		errs = append(errs, fmt.Errorf("webhook-backoff-max (%s) must be at least webhook-backoff-base (%s)", c.WebhookBackoffMax, c.WebhookBackoffBase))
	}
	if c.WebhookURL != "" {
		if u, err := url.Parse(c.WebhookURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			errs = append(errs, fmt.Errorf("webhook-url %q must be an http or https URL", c.WebhookURL))
		}
	}
	return errs
}

func (c Config) validateSources() []error {
	var errs []error
	seen := make(map[string]bool)
	for _, spec := range c.ResolvedSources() {
		label := spec.EffectiveLabel()
		if seen[label] {
			errs = append(errs, fmt.Errorf("duplicate source label %q: labels must be unique", label))
		}
		seen[label] = true
	}
	return errs
}
