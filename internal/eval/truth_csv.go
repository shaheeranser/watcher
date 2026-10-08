package eval

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
)

// decodeCSV reads a header row naming at least id and expected_cause, with an
// optional kind. Unknown columns are ignored so the format can grow without
// breaking older readers (design §3.1).
func decodeCSV(data []byte) ([]Case, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1 // tolerate extra/missing columns; validate below
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("truth CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("truth CSV is empty")
	}

	header := rows[0]
	idCol := columnIndex(header, "id")
	causeCol := columnIndex(header, "expected_cause")
	kindCol := columnIndex(header, "kind")
	if idCol < 0 || causeCol < 0 {
		return nil, errors.New("truth CSV header must include id and expected_cause columns")
	}

	cases := make([]Case, 0, len(rows)-1)
	for _, row := range rows[1:] {
		if len(row) == 1 && strings.TrimSpace(row[0]) == "" {
			continue
		}
		cases = append(cases, Case{
			ID:            field(row, idCol),
			ExpectedCause: field(row, causeCol),
			Kind:          field(row, kindCol),
		})
	}
	return cases, nil
}

func columnIndex(header []string, name string) int {
	for i, h := range header {
		if strings.EqualFold(strings.TrimSpace(h), name) {
			return i
		}
	}
	return -1
}

func field(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return row[index]
}
