package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/shaheeranser/watcher/internal/config"
	"github.com/shaheeranser/watcher/internal/eval"
)

const evalUsage = `watcher eval scores Watcher's explanations against external ground truth.

Usage:
  watcher eval --truth FILE --results FILE|-

Flags (environment variable in parentheses):
  --truth FILE          ground-truth file, JSON or CSV (WATCHER_EVAL_TRUTH)
  --results FILE|-      Watcher's JSON Lines results, or - for stdin (WATCHER_EVAL_RESULTS)
  --identifier MODE     fingerprint or run_id (WATCHER_EVAL_IDENTIFIER; default run_id)
  --truth-format FMT    json or csv, overriding the file extension (WATCHER_EVAL_TRUTH_FORMAT)
  --threshold TAU       similarity threshold, 0..1 (WATCHER_EVAL_THRESHOLD; default 0.60)
  --min-pass-rate RATE  exit non-zero below this pass rate (WATCHER_EVAL_MIN_PASS_RATE)
  --require-kind        fail a case whose detector kind differs (WATCHER_EVAL_REQUIRE_KIND)
  --format FMT          text, json, or both (WATCHER_EVAL_FORMAT; default text)

Exit code is 0 when the pass rate meets --min-pass-rate, non-zero otherwise, and
2 for a usage or input error. It reads a captured run; it never calls the model.`

// evalCommand scores Watcher's own JSON Lines output against a ground-truth file
// and reports a tally, a JSON report, and a CI exit code. It performs no
// detection and no model calls (EVAL-SCORE-8).
func evalCommand(args []string) int {
	cfg, err := config.ParseEval(args, os.Getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, evalUsage)
			return 0
		}
		fmt.Fprintf(os.Stderr, "watcher eval: %v\n", err)
		return 2
	}

	cases, err := eval.LoadTruth(cfg.TruthPath, eval.Format(cfg.TruthFormat))
	if err != nil {
		fmt.Fprintf(os.Stderr, "watcher eval: %v\n", err)
		return 2
	}

	results, closeResults, err := openResults(cfg.ResultsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watcher eval: %v\n", err)
		return 2
	}
	defer closeResults()

	parsed, err := eval.ReadResults(results)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watcher eval: %v\n", err)
		return 2
	}
	if parsed.Junk > 0 {
		fmt.Fprintf(os.Stderr, "watcher eval: skipped %d unparseable line(s)\n", parsed.Junk)
	}

	var minPassRate *float64
	if cfg.MinPassRateSet {
		minPassRate = &cfg.MinPassRate
	}
	report := eval.Score(eval.Options{
		Cases:       cases,
		Results:     parsed.Incidents,
		Identifier:  eval.Identifier(cfg.Identifier),
		Threshold:   cfg.Threshold,
		RequireKind: cfg.RequireKind,
		MinPassRate: minPassRate,
		Junk:        parsed.Junk,
	})

	if err := writeReport(report, cfg.Format); err != nil {
		fmt.Fprintf(os.Stderr, "watcher eval: %v\n", err)
		return 1
	}
	if report.Verdict != "pass" {
		return 1
	}
	return 0
}

// openResults returns the results reader, treating "-" as stdin so
// `watcher … | watcher eval …` works (EVAL-ACT-2).
func openResults(path string) (io.Reader, func(), error) {
	if path == "-" {
		return os.Stdin, func() {}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open results: %w", err)
	}
	return file, func() { file.Close() }, nil
}

func writeReport(report eval.Report, format string) error {
	switch format {
	case "json":
		data, err := report.JSON()
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(data)
		return err
	case "both":
		fmt.Fprint(os.Stdout, report.Tally())
		data, err := report.JSON()
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout)
		_, err = os.Stdout.Write(data)
		return err
	default:
		_, err := fmt.Fprint(os.Stdout, report.Tally())
		return err
	}
}
