package eval

import (
	"encoding/json"
	"fmt"
	"sort"
)

// decodeJSON accepts the two documented shapes: a bare object mapping an id to
// its expected cause, or an object with a "cases" array of
// {id, expected_cause, kind?} entries (design §3.1).
func decodeJSON(data []byte) ([]Case, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("truth file is not a JSON object: %w", err)
	}
	if raw, ok := top["cases"]; ok {
		return decodeJSONCases(raw)
	}
	return decodeJSONMap(top)
}

type jsonCase struct {
	ID            string `json:"id"`
	ExpectedCause string `json:"expected_cause"`
	Kind          string `json:"kind"`
}

func decodeJSONCases(raw json.RawMessage) ([]Case, error) {
	var entries []jsonCase
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("truth \"cases\" is not an array of cases: %w", err)
	}
	cases := make([]Case, 0, len(entries))
	for _, e := range entries {
		cases = append(cases, Case{ID: e.ID, ExpectedCause: e.ExpectedCause, Kind: e.Kind})
	}
	return cases, nil
}

func decodeJSONMap(top map[string]json.RawMessage) ([]Case, error) {
	ids := make([]string, 0, len(top))
	for id := range top {
		ids = append(ids, id)
	}
	// Sorting keeps the report deterministic: Go map iteration is randomized
	// (EVAL-SCORE-7).
	sort.Strings(ids)
	cases := make([]Case, 0, len(ids))
	for _, id := range ids {
		var cause string
		if err := json.Unmarshal(top[id], &cause); err != nil {
			return nil, fmt.Errorf("case %q: expected_cause must be a string: %w", id, err)
		}
		cases = append(cases, Case{ID: id, ExpectedCause: cause})
	}
	return cases, nil
}
