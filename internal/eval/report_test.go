package eval

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func loadCorpus(t *testing.T, truthFile string) Options {
	t.Helper()
	cases, err := LoadTruth(filepath.Join("testdata", truthFile), "")
	if err != nil {
		t.Fatalf("LoadTruth: %v", err)
	}
	results, err := os.Open(filepath.Join("testdata", "results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer results.Close()
	parsed, err := ReadResults(results)
	if err != nil {
		t.Fatalf("ReadResults: %v", err)
	}
	min := 0.60
	return Options{
		Cases:       cases,
		Results:     parsed.Incidents,
		Identifier:  ByRunID,
		Threshold:   0.60,
		MinPassRate: &min,
		Junk:        parsed.Junk,
	}
}

func TestTallyGolden(t *testing.T) {
	report := Score(loadCorpus(t, "truth.json"))
	checkGolden(t, "tally.golden", report.Tally())
}

func TestReportJSONGolden(t *testing.T) {
	report := Score(loadCorpus(t, "truth.json"))
	data, err := report.JSON()
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "report.golden.json", string(data))
}

func TestJSONAndCSVTruthAgree(t *testing.T) {
	fromJSON := Score(loadCorpus(t, "truth.json"))
	fromCSV := Score(loadCorpus(t, "truth.csv"))
	if fromJSON.Tally() != fromCSV.Tally() {
		t.Errorf("JSON and CSV truth produced different tallies\n--- json ---\n%s\n--- csv ---\n%s", fromJSON.Tally(), fromCSV.Tally())
	}
}

func TestReportCategoriesCoverEveryCase(t *testing.T) {
	report := Score(loadCorpus(t, "truth.json"))
	if report.Total != len(report.Cases) {
		t.Fatalf("total = %d, cases = %d", report.Total, len(report.Cases))
	}
	if report.Passed+report.Failed != report.Total {
		t.Fatalf("passed+failed = %d, total = %d", report.Passed+report.Failed, report.Total)
	}

	seen := map[Category]bool{}
	for _, c := range report.Cases {
		seen[c.Category] = true
	}
	for _, want := range []Category{CategoryPass, CategoryCauseMismatch, CategoryNoExplanation, CategoryNoResult, CategoryKindMismatch} {
		if !seen[want] {
			t.Errorf("the corpus should exercise %q", want)
		}
	}
}
