package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan Line) Line {
	t.Helper()
	select {
	case line, ok := <-ch:
		if !ok {
			t.Fatal("source closed before delivering a line")
		}
		return line
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a line")
		return Line{}
	}
}

func expectNoLine(t *testing.T, ch <-chan Line) {
	t.Helper()
	select {
	case line, ok := <-ch:
		if !ok {
			t.Fatal("source closed before delivering a line")
		}
		t.Fatalf("unexpected line %q", line.Raw)
	case <-time.After(150 * time.Millisecond):
	}
}

func expectClosed(t *testing.T, ch <-chan Line) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected the channel to close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for close")
	}
}

func TestStdinStreamsBeforeEOF(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := NewStdin(r, "", nil).Stream(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.WriteString("first line\n"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, ch).Raw; got != "first line" {
		t.Errorf("Raw = %q, want %q", got, "first line")
	}
	// The write handle is still open, so delivery must not have waited for EOF.
	if _, err := w.WriteString("second line\n"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, ch).Raw; got != "second line" {
		t.Errorf("Raw = %q, want %q", got, "second line")
	}
	w.Close()
	expectClosed(t, ch)
}

func TestStdinVeryLongLineIsNotTruncated(t *testing.T) {
	long := strings.Repeat("x", 100*1024)
	ch, err := NewStdin(strings.NewReader(long+"\n"), "", nil).Stream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := recv(t, ch)
	if got.Raw != long {
		t.Errorf("line length = %d, want %d", len(got.Raw), len(long))
	}
	expectClosed(t, ch)
}

func TestStdinFlushesFinalLineWithoutNewline(t *testing.T) {
	ch, err := NewStdin(strings.NewReader("no trailing newline"), "", nil).Stream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := recv(t, ch).Raw; got != "no trailing newline" {
		t.Errorf("Raw = %q", got)
	}
	expectClosed(t, ch)
}

func TestStdinStripsCarriageReturns(t *testing.T) {
	ch, err := NewStdin(strings.NewReader("a\r\nb\r\n"), "", nil).Stream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a", "b"} {
		if got := recv(t, ch).Raw; got != want {
			t.Errorf("Raw = %q, want %q", got, want)
		}
	}
	expectClosed(t, ch)
}

func TestStdinStopsOnCancellation(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := NewStdin(r, "", nil).Stream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	expectClosed(t, ch)
}

func TestFileStartsAtEndByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := NewFile(path, "", false, nil)
	f.pollInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := f.Stream(ctx)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond)
	appendFile(t, path, "new\n")

	if got := recv(t, ch).Raw; got != "new" {
		t.Errorf("first line = %q, want %q (pre-existing content must be skipped)", got, "new")
	}
}

func TestFileCanStartFromBeginning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := NewFile(path, "", true, nil)
	f.pollInterval = 10 * time.Millisecond
	ch, err := f.Stream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := recv(t, ch).Raw; got != "old" {
		t.Errorf("first line = %q, want %q", got, "old")
	}
}

func TestFileFollowsRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := NewFile(path, "", false, nil)
	f.pollInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := f.Stream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)

	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := recv(t, ch).Raw; got != "after" {
		t.Errorf("line after rotation = %q, want %q", got, "after")
	}
}

func TestFileWaitsForLateAppearance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "later.log")

	f := NewFile(path, "", true, nil)
	f.pollInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := f.Stream(ctx)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path, []byte("created\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, ch).Raw; got != "created" {
		t.Errorf("line = %q, want %q", got, "created")
	}
}

func TestFileStopsOnCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := NewFile(path, "", true, nil)
	f.pollInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := f.Stream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recv(t, ch)
	cancel()
	expectClosed(t, ch)
}

func TestFileRejectsEmptyPath(t *testing.T) {
	if _, err := NewFile("", "", false, nil).Stream(context.Background()); err == nil {
		t.Fatal("expected an error for an empty path")
	}
}

func TestStdinCarriesLabel(t *testing.T) {
	ch, err := NewStdin(strings.NewReader("hello\n"), "backend", nil).Stream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	line := recv(t, ch)
	if line.Source != "backend" {
		t.Errorf("Source = %q, want backend", line.Source)
	}
	if got := NewStdin(strings.NewReader(""), "", nil).Name(); got != "stdin" {
		t.Errorf("default stdin name = %q, want stdin", got)
	}
}

func TestFileCarriesLabelAndDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("crash\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := NewFile(path, "", true, nil).Name(); got != path {
		t.Errorf("default file name = %q, want the path %q", got, path)
	}

	f := NewFile(path, "worker", true, nil)
	f.pollInterval = 10 * time.Millisecond
	if f.Name() != "worker" || f.Path() != path {
		t.Fatalf("Name/Path = %q/%q, want worker/%q", f.Name(), f.Path(), path)
	}
	ch, err := f.Stream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := recv(t, ch).Source; got != "worker" {
		t.Errorf("Source = %q, want worker", got)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}
