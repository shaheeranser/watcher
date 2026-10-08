// Package eval scores Watcher's own explanations against an external
// ground-truth file. It is a pure, offline second entry point: it performs no
// detection, no fingerprinting, and no model calls, because everything it needs
// is already in the ground-truth file and the captured results stream
// (EVAL-NFR-1, EVAL-NFR-3).
package eval

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Case is one ground-truth entry: the identifier a result is matched on, the
// cause a human says is correct, and the detector kind the case exercises when
// it is known.
type Case struct {
	ID            string
	ExpectedCause string
	Kind          string
}

// Format selects the ground-truth file shape.
type Format string

const (
	FormatJSON Format = "json"
	FormatCSV  Format = "csv"
)

// ParseFormat validates an explicit --truth-format value.
func ParseFormat(raw string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(raw))) {
	case FormatJSON:
		return FormatJSON, nil
	case FormatCSV:
		return FormatCSV, nil
	default:
		return "", fmt.Errorf("unknown truth format %q: want json or csv", raw)
	}
}

// LoadTruth reads and validates a ground-truth file. An empty format infers the
// shape from the file extension. It fails with a specific error when the file
// is unreadable, empty, malformed, missing a required field, or carries a
// duplicate identifier (EVAL-GT-1, EVAL-GT-4, EVAL-GT-5).
func LoadTruth(path string, format Format) ([]Case, error) {
	if format == "" {
		detected, err := detectFormat(path)
		if err != nil {
			return nil, err
		}
		format = detected
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read truth file: %w", err)
	}
	cases, err := decodeTruth(data, format)
	if err != nil {
		return nil, err
	}
	if err := validateCases(cases); err != nil {
		return nil, err
	}
	return cases, nil
}

// detectFormat infers the ground-truth shape from the file extension.
func detectFormat(path string) (Format, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return FormatJSON, nil
	case ".csv":
		return FormatCSV, nil
	default:
		return "", fmt.Errorf("cannot infer truth format from %q: pass --truth-format json|csv", path)
	}
}

func decodeTruth(data []byte, format Format) ([]Case, error) {
	switch format {
	case FormatJSON:
		return decodeJSON(data)
	case FormatCSV:
		return decodeCSV(data)
	default:
		return nil, fmt.Errorf("unknown truth format %q", format)
	}
}

// validateCases reports every problem at once so a malformed corpus is fixed in
// one pass rather than one error at a time (EVAL-GT-4, EVAL-GT-5).
func validateCases(cases []Case) error {
	if len(cases) == 0 {
		return errors.New("truth file is empty: no cases defined")
	}
	var errs []error
	seen := make(map[string]bool, len(cases))
	for i, c := range cases {
		if strings.TrimSpace(c.ID) == "" {
			errs = append(errs, fmt.Errorf("case %d has an empty id", i+1))
			continue
		}
		if strings.TrimSpace(c.ExpectedCause) == "" {
			errs = append(errs, fmt.Errorf("case %q has an empty expected_cause", c.ID))
		}
		if seen[c.ID] {
			errs = append(errs, fmt.Errorf("duplicate id %q", c.ID))
		}
		seen[c.ID] = true
	}
	return errors.Join(errs...)
}
