package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"
)

// AttachConfig is what `watcher attach` needs to reach a daemon and render it.
// It is deliberately separate from Config: attach neither watches logs nor calls
// a model, so requiring --model for it would be wrong.
type AttachConfig struct {
	APISocket       string
	RefreshInterval time.Duration
	ListFraction    float64
}

// ParseAttach resolves the attach settings with the same flag > environment >
// default precedence the daemon uses. getenv is injected so tests can supply a
// fake environment.
func ParseAttach(args []string, getenv func(string) string) (AttachConfig, error) {
	cfg := AttachConfig{
		APISocket:       defaultSocket(getenv),
		RefreshInterval: DefaultRefreshInterval,
		ListFraction:    DefaultListFraction,
	}
	if v := getenv("WATCHER_API_SOCKET"); v != "" {
		cfg.APISocket = v
	}
	refresh, err := envDuration(getenv, "WATCHER_REFRESH_INTERVAL", cfg.RefreshInterval)
	if err != nil {
		return AttachConfig{}, err
	}
	cfg.RefreshInterval = refresh
	fraction, err := envFloat(getenv, "WATCHER_LIST_FRACTION", cfg.ListFraction)
	if err != nil {
		return AttachConfig{}, err
	}
	cfg.ListFraction = fraction

	fs := flag.NewFlagSet("watcher attach", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	apiSocket := fs.String("api-socket", cfg.APISocket, "unix socket path of the running daemon")
	refreshInterval := fs.Duration("refresh-interval", cfg.RefreshInterval, "bound on how stale the UI may be, and the polling fallback interval")
	listFraction := fs.Float64("list-fraction", cfg.ListFraction, "share of terminal height used by the incident list")
	if err := fs.Parse(args); err != nil {
		return AttachConfig{}, fmt.Errorf("parse flags: %w", err)
	}
	cfg.APISocket = *apiSocket
	cfg.RefreshInterval = *refreshInterval
	cfg.ListFraction = *listFraction

	if err := cfg.Validate(); err != nil {
		return AttachConfig{}, err
	}
	return cfg, nil
}

// Validate reports every problem at once, matching the daemon's behaviour.
func (c AttachConfig) Validate() error {
	var errs []error
	if c.APISocket == "" {
		errs = append(errs, errors.New("api-socket must not be empty"))
	}
	if c.RefreshInterval <= 0 {
		errs = append(errs, fmt.Errorf("refresh-interval must be positive, got %s", c.RefreshInterval))
	}
	if c.ListFraction <= 0 || c.ListFraction >= 1 {
		errs = append(errs, fmt.Errorf("list-fraction must be between 0 and 1, got %g", c.ListFraction))
	}
	return errors.Join(errs...)
}

func envFloat(getenv func(string) string, name string, def float64) (float64, error) {
	raw := getenv(name)
	if raw == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("environment %s: %w", name, err)
	}
	return f, nil
}
