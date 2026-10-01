package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaheeranser/watcher/internal/source"
)

func TestTripsWhenWeAreTheOnlyWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "own-output.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	self, reason := IsSelf(source.NewFile(path, "", false, nil))
	if !self {
		t.Fatalf("guard should trip when Watcher is the file's only writer")
	}
	if !strings.Contains(reason, "writer") {
		t.Errorf("reason = %q, want it to mention the writer", reason)
	}
}

func TestAllowsUnrelatedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if self, reason := IsSelf(source.NewFile(path, "", false, nil)); self {
		t.Fatalf("guard tripped on an unrelated file: %s", reason)
	}
}

func TestAllowsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-yet.log")
	if self, _ := IsSelf(source.NewFile(path, "", false, nil)); self {
		t.Fatal("guard must not trip on a file that does not exist yet")
	}
}

func TestAllowsStdin(t *testing.T) {
	if self, _ := IsSelf(source.NewStdin(strings.NewReader(""), "", nil)); self {
		t.Fatal("guard must not trip on stdin")
	}
}

func TestOnlySelf(t *testing.T) {
	me := os.Getpid()
	if !onlySelf([]int{me, me}) {
		t.Error("a list containing only our pid is self")
	}
	if onlySelf([]int{me, me + 1}) {
		t.Error("a list with another process is not self")
	}
}

func TestSameContainer(t *testing.T) {
	full := strings.Repeat("a", 64)
	if !SameContainer(full, full[:12]) {
		t.Error("a 64-character id must match its 12-character short form")
	}
	if !SameContainer(full, full) {
		t.Error("an id must match itself")
	}
	if SameContainer(full, strings.Repeat("b", 64)) {
		t.Error("different ids must not match")
	}
	if SameContainer("short", "short") {
		t.Error("ids shorter than the short form are not comparable")
	}
}

func TestIsSelfContainerIgnoresEmpty(t *testing.T) {
	if self, _ := IsSelfContainer(""); self {
		t.Error("an empty container id is never self")
	}
}
