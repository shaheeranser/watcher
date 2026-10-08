package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
)

// DefaultEvalThreshold is the default similarity cutoff tau. It is the spec's
// example value and is not calibrated against a real corpus, so a caller that
// cares about the exact cutoff sets --threshold (OD-04-1).
const DefaultEvalThreshold = 0.60

// DefaultEvalIdentifier keys ground truth on the run id, the recommended mode
// because it needs no circular fingerprint discovery (design §2.2).
const DefaultEvalIdentifier = "run_id"

// EvalConfig is what `watcher eval` needs to score a captured run. It is
// deliberately separate from Config: scoring neither watches logs nor calls a
// model, so requiring --model for it would be wrong.
type EvalConfig struct {
	TruthPath      string
	ResultsPath    string
	TruthFormat    string
	Identifier     string
	Threshold      float64
	MinPassRate    float64
	MinPassRateSet bool
	RequireKind    bool
	Format         string
}

// ParseEval resolves the scoring settings with the same flag > environment >
// default precedence the daemon uses. getenv is injected so tests can supply a
// fake environment.
func ParseEval(args []string, getenv func(string) string) (EvalConfig, error) {
	cfg := EvalConfig{
		TruthPath:   getenv("WATCHER_EVAL_TRUTH"),
		ResultsPath: getenv("WATCHER_EVAL_RESULTS"),
		TruthFormat: getenv("WATCHER_EVAL_TRUTH_FORMAT"),
		Identifier:  DefaultEvalIdentifier,
		Threshold:   DefaultEvalThreshold,
		Format:      "text",
	}
	if v := getenv("WATCHER_EVAL_IDENTIFIER"); v != "" {
		cfg.Identifier = v
	}
	if v := getenv("WATCHER_EVAL_FORMAT"); v != "" {
		cfg.Format = v
	}
	threshold, err := envFloat(getenv, "WATCHER_EVAL_THRESHOLD", cfg.Threshold)
	if err != nil {
		return EvalConfig{}, err
	}
	cfg.Threshold = threshold

	// A negative sentinel means the environment did not configure a gate.
	minPassRate, err := envFloat(getenv, "WATCHER_EVAL_MIN_PASS_RATE", -1)
	if err != nil {
		return EvalConfig{}, err
	}
	if minPassRate >= 0 {
		cfg.MinPassRate = minPassRate
		cfg.MinPassRateSet = true
	}
	requireKind, err := envBool(getenv, "WATCHER_EVAL_REQUIRE_KIND", false)
	if err != nil {
		return EvalConfig{}, err
	}
	cfg.RequireKind = requireKind

	fs := flag.NewFlagSet("watcher eval", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	truth := fs.String("truth", cfg.TruthPath, "ground-truth file (JSON or CSV)")
	results := fs.String("results", cfg.ResultsPath, "results JSON Lines file, or '-' for stdin")
	truthFormat := fs.String("truth-format", cfg.TruthFormat, "override the inferred truth format: json or csv")
	identifier := fs.String("identifier", cfg.Identifier, "correlation identifier: fingerprint or run_id")
	thresholdFlag := fs.Float64("threshold", cfg.Threshold, "similarity threshold tau")
	requireKindFlag := fs.Bool("require-kind", cfg.RequireKind, "fail a case whose detector kind differs")
	format := fs.String("format", cfg.Format, "output format: text, json, or both")

	// A function flag distinguishes "not supplied" from "supplied as zero", so
	// an unset --min-pass-rate stays report-only (OD-04-2).
	minPassRate = cfg.MinPassRate
	minPassSet := cfg.MinPassRateSet
	fs.Func("min-pass-rate", "exit non-zero below this pass rate", func(raw string) error {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		minPassRate = v
		minPassSet = true
		return nil
	})

	if err := fs.Parse(args); err != nil {
		return EvalConfig{}, fmt.Errorf("parse flags: %w", err)
	}
	cfg.TruthPath = *truth
	cfg.ResultsPath = *results
	cfg.TruthFormat = *truthFormat
	cfg.Identifier = *identifier
	cfg.Threshold = *thresholdFlag
	cfg.RequireKind = *requireKindFlag
	cfg.Format = *format
	cfg.MinPassRate = minPassRate
	cfg.MinPassRateSet = minPassSet

	if err := cfg.Validate(); err != nil {
		return EvalConfig{}, err
	}
	return cfg, nil
}

// Validate reports every problem at once, matching the daemon's behaviour.
func (c EvalConfig) Validate() error {
	var errs []error
	if c.TruthPath == "" {
		errs = append(errs, errors.New("no ground truth: set --truth"))
	}
	if c.ResultsPath == "" {
		errs = append(errs, errors.New("no results: set --results (or '-' for stdin)"))
	}
	switch c.TruthFormat {
	case "", "json", "csv":
	default:
		errs = append(errs, fmt.Errorf("truth-format %q is not one of json, csv", c.TruthFormat))
	}
	switch c.Identifier {
	case "fingerprint", "run_id":
	default:
		errs = append(errs, fmt.Errorf("identifier %q is not one of fingerprint, run_id", c.Identifier))
	}
	if c.Threshold < 0 || c.Threshold > 1 {
		errs = append(errs, fmt.Errorf("threshold must be between 0 and 1, got %g", c.Threshold))
	}
	if c.MinPassRateSet && (c.MinPassRate < 0 || c.MinPassRate > 1) {
		errs = append(errs, fmt.Errorf("min-pass-rate must be between 0 and 1, got %g", c.MinPassRate))
	}
	switch c.Format {
	case "text", "json", "both":
	default:
		errs = append(errs, fmt.Errorf("format %q is not one of text, json, both", c.Format))
	}
	return errors.Join(errs...)
}
