package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []Case
		wantErr string
	}{
		{
			name:  "cases array with kind",
			input: `{"cases":[{"id":"run-001","expected_cause":"nil deref","kind":"go-panic"},{"id":"run-002","expected_cause":"key error"}]}`,
			want:  []Case{{ID: "run-001", ExpectedCause: "nil deref", Kind: "go-panic"}, {ID: "run-002", ExpectedCause: "key error"}},
		},
		{
			name:  "bare object map is sorted deterministically",
			input: `{"run-002":"second","run-001":"first"}`,
			want:  []Case{{ID: "run-001", ExpectedCause: "first"}, {ID: "run-002", ExpectedCause: "second"}},
		},
		{
			name:  "unicode and unknown fields are kept and ignored",
			input: `{"cases":[{"id":"rün-1","expected_cause":"café ✦ đ","kind":"generic-fatal","extra":"ignored"}]}`,
			want:  []Case{{ID: "rün-1", ExpectedCause: "café ✦ đ", Kind: "generic-fatal"}},
		},
		{
			name:    "not an object",
			input:   `[]`,
			wantErr: "not a JSON object",
		},
		{
			name:    "cases is not an array",
			input:   `{"cases":{}}`,
			wantErr: "not an array",
		},
		{
			name:    "map value is not a string",
			input:   `{"run-1":42}`,
			wantErr: "must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeJSON([]byte(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeJSON: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("cases = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("case %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDecodeCSV(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []Case
		wantErr string
	}{
		{
			name:  "header with optional kind and unknown column",
			input: "id,expected_cause,kind,notes\nrun-001,plain,go-panic,x\n",
			want:  []Case{{ID: "run-001", ExpectedCause: "plain", Kind: "go-panic"}},
		},
		{
			name:  "quoted commas and blank lines",
			input: "id,expected_cause\nrun-002,\"a, b, and c\"\n\nrun-003,simple\n",
			want:  []Case{{ID: "run-002", ExpectedCause: "a, b, and c"}, {ID: "run-003", ExpectedCause: "simple"}},
		},
		{
			name:  "kind column absent",
			input: "id,expected_cause\nrun-004,cause\n",
			want:  []Case{{ID: "run-004", ExpectedCause: "cause"}},
		},
		{
			name:    "missing required column",
			input:   "id,kind\nrun-1,go-panic\n",
			wantErr: "must include id and expected_cause",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeCSV([]byte(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeCSV: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("cases = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("case %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestValidateCases(t *testing.T) {
	tests := []struct {
		name    string
		cases   []Case
		wantErr string
	}{
		{"empty file", nil, "empty"},
		{"empty id", []Case{{ID: "  ", ExpectedCause: "x"}}, "empty id"},
		{"empty cause", []Case{{ID: "a", ExpectedCause: ""}}, "empty expected_cause"},
		{"duplicate id", []Case{{ID: "a", ExpectedCause: "x"}, {ID: "a", ExpectedCause: "y"}}, "duplicate id"},
		{"valid", []Case{{ID: "a", ExpectedCause: "x"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCases(tt.cases)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCasesReportsAllProblems(t *testing.T) {
	err := validateCases([]Case{
		{ID: "", ExpectedCause: "x"},
		{ID: "b", ExpectedCause: ""},
		{ID: "b", ExpectedCause: "y"},
		{ID: "b", ExpectedCause: "z"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"empty id", "empty expected_cause", "duplicate id"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestLoadTruthDetectsFormatByExtension(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "truth.json")
	csvPath := filepath.Join(dir, "truth.csv")
	if err := os.WriteFile(jsonPath, []byte(`{"run-1":"cause one"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(csvPath, []byte("id,expected_cause\nrun-1,cause one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fromJSON, err := LoadTruth(jsonPath, "")
	if err != nil {
		t.Fatalf("LoadTruth json: %v", err)
	}
	fromCSV, err := LoadTruth(csvPath, "")
	if err != nil {
		t.Fatalf("LoadTruth csv: %v", err)
	}
	if len(fromJSON) != 1 || fromJSON[0].ExpectedCause != "cause one" {
		t.Errorf("json cases = %+v", fromJSON)
	}
	if len(fromCSV) != 1 || fromCSV[0].ExpectedCause != "cause one" {
		t.Errorf("csv cases = %+v", fromCSV)
	}

	if _, err := LoadTruth(filepath.Join(dir, "truth.txt"), ""); err == nil {
		t.Error("expected an error for an unknown extension")
	}
}

func TestLoadTruthExplicitFormatOverridesExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "truth.txt")
	if err := os.WriteFile(path, []byte("id,expected_cause\nrun-1,cause\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTruth(path, FormatCSV); err != nil {
		t.Errorf("explicit CSV format should parse a .txt file: %v", err)
	}
}

func TestLoadTruthEmptyFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "truth.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTruth(path, ""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error = %v, want an empty-file error", err)
	}
}
