package detect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaheeranser/watcher/internal/source"
)

func detectAll(t *testing.T, tailLines, maxBlock int, lines []string) []Event {
	t.Helper()
	in := make(chan source.Line, len(lines))
	for _, l := range lines {
		in <- source.Line{Raw: l, Source: "test"}
	}
	close(in)

	out := make(chan Event, 64)
	d := New(tailLines, maxBlock)
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Detect(context.Background(), in, out)
		close(out)
	}()

	var events []Event
	for e := range out {
		events = append(events, e)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return events
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func blockText(block []source.Line) string {
	raws := make([]string, len(block))
	for i, l := range block {
		raws[i] = l.Raw
	}
	return strings.Join(raws, "\n")
}

func TestGoldenBlocks(t *testing.T) {
	cases := []struct {
		fixture string
		kind    string
	}{
		{"go_panic", KindGoPanic},
		{"python_traceback", KindPythonTraceback},
		{"generic_fatal", KindGenericFatal},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			lines := readLines(t, filepath.Join("testdata", tc.fixture+".log"))
			events := detectAll(t, 10, 200, lines)
			if len(events) != 1 {
				t.Fatalf("events = %d, want 1", len(events))
			}
			if events[0].Kind != tc.kind {
				t.Errorf("Kind = %q, want %q", events[0].Kind, tc.kind)
			}
			want := strings.TrimSuffix(string(readRaw(t, filepath.Join("testdata", tc.fixture+".golden"))), "\n")
			if got := blockText(events[0].Block); got != want {
				t.Errorf("block mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

func readRaw(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNoMatchEmitsNothing(t *testing.T) {
	lines := readLines(t, filepath.Join("testdata", "no_match.log"))
	if events := detectAll(t, 10, 200, lines); len(events) != 0 {
		t.Fatalf("events = %d, want 0", len(events))
	}
}

func TestTailIsCapturedAfterBlock(t *testing.T) {
	lines := []string{
		"Traceback (most recent call last):",
		`  File "a.py", line 1, in <module>`,
		`ValueError: x`,
		"tail one",
		"tail two",
		"tail three",
	}
	events := detectAll(t, 2, 200, lines)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if len(events[0].Tail) != 2 {
		t.Fatalf("tail length = %d, want 2", len(events[0].Tail))
	}
	if events[0].Tail[0].Raw != "tail one" || events[0].Tail[1].Raw != "tail two" {
		t.Errorf("unexpected tail: %v", events[0].Tail)
	}
}

func TestNewTriggerEndsPreviousBlock(t *testing.T) {
	lines := []string{
		"panic: first",
		"goroutine 1 [running]:",
		"\tmain.a()",
		"\t/a.go:1 +0x0",
		"panic: second",
		"goroutine 2 [running]:",
		"\tmain.b()",
		"\t/b.go:2 +0x0",
	}
	events := detectAll(t, 0, 200, lines)
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Trigger.Raw != "panic: first" || events[1].Trigger.Raw != "panic: second" {
		t.Errorf("unexpected triggers: %q, %q", events[0].Trigger.Raw, events[1].Trigger.Raw)
	}
}

func TestBlockTruncationKeepsEdges(t *testing.T) {
	lines := []string{"panic: boom", "goroutine 1 [running]:"}
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("main.f%d()", i), fmt.Sprintf("\tf%d.go:%d +0x0", i, i))
	}

	events := detectAll(t, 0, 6, lines)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	block := events[0].Block
	if len(block) != 6 {
		t.Fatalf("block length = %d, want 6", len(block))
	}
	if block[0].Raw != "panic: boom" {
		t.Errorf("trigger = %q, want %q", block[0].Raw, "panic: boom")
	}
	if !strings.Contains(block[3].Raw, "elided") {
		t.Errorf("expected an elision marker at index 3, got %q", block[3].Raw)
	}
	if last := lines[len(lines)-1]; block[5].Raw != last {
		t.Errorf("last line = %q, want %q", block[5].Raw, last)
	}
}

func TestDetectStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	in := make(chan source.Line)
	out := make(chan Event)
	done := make(chan error, 1)
	go func() { done <- New(0, 200).Detect(ctx, in, out) }()

	if err := <-done; err != nil {
		t.Fatalf("Detect returned %v, want nil on cancellation", err)
	}
}

func TestIdleFlushEmitsBlockWhenStreamGoesQuiet(t *testing.T) {
	d := New(10, 200)
	d.idle = 20 * time.Millisecond

	in := make(chan source.Line, 8)
	out := make(chan Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Detect(ctx, in, out) }()

	for _, raw := range []string{"panic: boom", "goroutine 1 [running]:", "\t/app/main.go:10 +0x1a", "exit status 2"} {
		in <- source.Line{Raw: raw, Source: "test"}
	}
	// The stream stays open and quiet, as a tailed file or container log does.
	select {
	case event := <-out:
		if event.Kind != KindGoPanic {
			t.Errorf("Kind = %q, want %q", event.Kind, KindGoPanic)
		}
		if event.Trigger.Raw != "panic: boom" {
			t.Errorf("Trigger = %q", event.Trigger.Raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a quiet finished block must be flushed, not held forever")
	}

	close(in)
	<-done
}

func TestIdleFlushIncludesLinesThatArriveBeforeIt(t *testing.T) {
	d := New(5, 200)
	d.idle = 40 * time.Millisecond

	in := make(chan source.Line, 8)
	out := make(chan Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Detect(ctx, in, out) }()

	for _, raw := range []string{"panic: boom", "goroutine 1 [running]:", "\t/app/main.go:10 +0x1a", "exit status 2", "post-crash note"} {
		in <- source.Line{Raw: raw, Source: "test"}
	}

	select {
	case event := <-out:
		found := false
		for _, line := range event.Tail {
			if line.Raw == "post-crash note" {
				found = true
			}
		}
		if !found {
			t.Errorf("tail = %v, want it to include the line that arrived before the idle flush", event.Tail)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the flushed block")
	}

	close(in)
	<-done
}

func TestIdleFlushDoesNotInterruptAnActiveBlock(t *testing.T) {
	d := New(5, 200)
	d.idle = 20 * time.Millisecond

	in := make(chan source.Line, 8)
	out := make(chan Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Detect(ctx, in, out) }()

	// A crash block that never reaches its terminator, then silence: the flush
	// must not fire mid-block.
	for _, raw := range []string{"panic: boom", "goroutine 1 [running]:", "\t/app/main.go:10 +0x1a"} {
		in <- source.Line{Raw: raw, Source: "test"}
	}
	select {
	case event := <-out:
		t.Fatalf("a block being collected must not be flushed early: %+v", event)
	case <-time.After(150 * time.Millisecond):
	}

	// Now the terminator arrives and the block completes normally.
	in <- source.Line{Raw: "exit status 2", Source: "test"}
	select {
	case event := <-out:
		if event.Kind != KindGoPanic {
			t.Errorf("Kind = %q", event.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("completing the block must emit it")
	}

	close(in)
	<-done
}
