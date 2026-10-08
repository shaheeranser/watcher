package eval

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestScoreIsDeterministic(t *testing.T) {
	first := Score(loadCorpus(t, "truth.json"))
	firstJSON, err := first.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		got := Score(loadCorpus(t, "truth.json"))
		gotJSON, err := got.JSON()
		if err != nil {
			t.Fatal(err)
		}
		if got.Tally() != first.Tally() {
			t.Fatalf("tally changed between identical runs:\n%s", got.Tally())
		}
		if string(gotJSON) != string(firstJSON) {
			t.Fatal("JSON report changed between identical runs")
		}
	}
}

func TestReThresholdKeepsScoresAndChangesVerdicts(t *testing.T) {
	cases := []Case{{ID: "run-1", ExpectedCause: "nil pointer dereference"}}
	results := []Result{{RunID: "run-1", Kind: "go-panic", LikelyCause: "nil pointer dereference in loader"}}

	base := Score(Options{Cases: cases, Results: results, Identifier: ByRunID, Threshold: 0.60})
	partial := base.Cases[0].Score
	if partial <= 0.60 || partial >= 1 {
		t.Fatalf("test needs a partial pass score, got %v", partial)
	}
	if base.Cases[0].Category != CategoryPass {
		t.Fatalf("case should pass at tau=0.60, got %q (score %v)", base.Cases[0].Category, partial)
	}

	strict := Score(Options{Cases: cases, Results: results, Identifier: ByRunID, Threshold: partial + 0.001})
	if strict.Cases[0].Category != CategoryCauseMismatch {
		t.Errorf("case should fail just above its score, got %q", strict.Cases[0].Category)
	}
	if strict.Cases[0].Score != base.Cases[0].Score {
		t.Errorf("re-thresholding changed the recorded score: %v vs %v", strict.Cases[0].Score, base.Cases[0].Score)
	}
}

func TestScoringPackageHasNoNetworkImports(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	banned := map[string]bool{"net": true, "net/http": true, "os/exec": true}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if banned[path] {
				t.Errorf("%s imports %q; scoring must stay offline (EVAL-SCORE-8)", name, path)
			}
		}
	}
}
