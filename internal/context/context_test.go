package context

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shaheeranser/watcher/internal/detect"
	"github.com/shaheeranser/watcher/internal/source"
)

func line(raw string, seq int) source.Line {
	return source.Line{Raw: raw, Source: "app.log", ArrivedAt: time.Unix(int64(seq), 0)}
}

func TestRingReturnsLinesBeforeTrigger(t *testing.T) {
	r := NewRing(16)
	for i := 0; i < 3; i++ {
		r.Add(line(fmt.Sprintf("before%d", i), i))
	}
	trigger := line("panic: boom", 3)
	r.Add(trigger)

	got := r.Preceding(trigger, 2)
	if len(got) != 2 {
		t.Fatalf("preceding length = %d, want 2", len(got))
	}
	if got[0].Raw != "before1" || got[1].Raw != "before2" {
		t.Errorf("unexpected preceding context: %v", got)
	}
}

func TestRingEvictsOldestWhenFull(t *testing.T) {
	r := NewRing(3)
	for i := 0; i <= 5; i++ {
		r.Add(line(fmt.Sprintf("l%d", i), i))
	}
	trigger := line("l5", 5)

	got := r.Preceding(trigger, 5)
	if len(got) != 2 || got[0].Raw != "l3" || got[1].Raw != "l4" {
		t.Errorf("preceding = %v, want [l3 l4]", got)
	}
}

func TestRingWithoutTriggerReturnsNothing(t *testing.T) {
	r := NewRing(4)
	r.Add(line("a", 0))
	if got := r.Preceding(line("missing", 9), 4); got != nil {
		t.Errorf("preceding = %v, want nil", got)
	}
}

func TestBuildIncludesHeaderWindowAndTail(t *testing.T) {
	ring := NewRing(16)
	ring.Add(line("before1", 0))
	ring.Add(line("before2", 1))
	trigger := line("panic: boom", 2)
	ring.Add(trigger)

	event := detect.Event{
		Kind:    detect.KindGoPanic,
		Trigger: trigger,
		Block:   []source.Line{trigger, line("goroutine 1 [running]:", 3), line("\t/app/main.go:10 +0x1a", 4)},
		Tail:    []source.Line{line("after line", 5)},
	}

	got := New(2, 8192, ring).Build(event)
	for _, want := range []string{"source: app.log", "detector: go-panic", "before1", "before2", "panic: boom", "after line", "\t/app/main.go:10 +0x1a"} {
		if !strings.Contains(got.Excerpt, want) {
			t.Errorf("excerpt missing %q\n%s", want, got.Excerpt)
		}
	}
}

func TestExcerptIsVerbatim(t *testing.T) {
	ring := NewRing(8)
	trigger := line("panic: request 550e8400-e29b-41d4-a716-446655440000 at 0x1a2b", 0)
	ring.Add(trigger)
	event := detect.Event{
		Kind:    detect.KindGoPanic,
		Trigger: trigger,
		Block:   []source.Line{trigger, line("\t/app/main.go:10 +0x1a", 1)},
	}
	got := New(0, 8192, ring).Build(event)
	if !strings.Contains(got.Excerpt, "550e8400-e29b-41d4-a716-446655440000") || !strings.Contains(got.Excerpt, "0x1a2b") {
		t.Errorf("excerpt must preserve volatile tokens verbatim\n%s", got.Excerpt)
	}
}

func TestReassembleMergesInterleavedStack(t *testing.T) {
	block := []source.Line{
		line("Traceback (most recent call last):", 0),
		line(`  File "a.py", line 1, in <module>`, 1),
	}
	tail := []source.Line{
		line("2026-01-02T03:04:05Z INFO unrelated", 2),
		line(`  File "b.py", line 2, in inner`, 3),
		line("ValueError: boom", 4),
	}

	curator := New(0, 8192, NewRing(8))
	got := curator.Build(detect.Event{Kind: detect.KindPythonTraceback, Trigger: block[0], Block: block, Tail: tail})

	var raws []string
	for _, l := range got.Block {
		raws = append(raws, l.Raw)
	}
	joined := strings.Join(raws, "\n")
	if !strings.Contains(joined, "…") {
		t.Errorf("expected an elision marker in the reassembled stack:\n%s", joined)
	}
	if !strings.Contains(joined, `File "b.py"`) {
		t.Errorf("resumed frame must be merged back into the stack:\n%s", joined)
	}
	if strings.Contains(joined, "unrelated") {
		t.Errorf("interleaved line must be dropped from the stack:\n%s", joined)
	}
	if !strings.Contains(got.Excerpt, "ValueError: boom") {
		t.Errorf("root exception line must be present:\n%s", got.Excerpt)
	}
}

func TestFitTrimsMiddleThenBeforeThenAfter(t *testing.T) {
	header := "source: app.log\ndetector: go-panic\n\n"
	before := make([]source.Line, 10)
	block := make([]source.Line, 50)
	after := make([]source.Line, 10)
	for i := range before {
		before[i] = line(fmt.Sprintf("ctx%02d", i), i)
	}
	for i := range block {
		block[i] = line(fmt.Sprintf("frame%02d", i), 100+i)
	}
	for i := range after {
		after[i] = line(fmt.Sprintf("tail%02d", i), 200+i)
	}

	const budget = 150
	gotBefore, gotBlock, gotAfter := fit(header, before, block, after, budget)

	if len(gotBlock) >= len(block) {
		t.Errorf("middle frames should be trimmed, block went %d -> %d", len(block), len(gotBlock))
	}
	if !strings.Contains(gotBlock[len(gotBlock)-2].Raw, "…") {
		t.Errorf("expected an elision marker before the root line, got %v", gotBlock)
	}
	if gotBlock[0].Raw != block[0].Raw {
		t.Errorf("trigger must be preserved")
	}
	if gotBlock[len(gotBlock)-1].Raw != block[len(block)-1].Raw {
		t.Errorf("root line must be preserved")
	}
	if size := excerptSize(header, gotBefore, gotBlock, gotAfter); size > budget {
		t.Errorf("excerpt size = %d, want <= %d", size, budget)
	}
	if len(gotAfter) >= len(after) && len(gotBefore) >= len(before) {
		t.Errorf("before and after were not reduced")
	}
}

func TestFitIsNoOpWhenWithinBudget(t *testing.T) {
	header := "h\n"
	before := []source.Line{line("a", 0)}
	block := []source.Line{line("b", 1)}
	after := []source.Line{line("c", 2)}

	gotBefore, gotBlock, gotAfter := fit(header, before, block, after, 4096)
	if len(gotBefore) != 1 || len(gotBlock) != 1 || len(gotAfter) != 1 {
		t.Errorf("within budget the excerpt must be untouched: %v %v %v", gotBefore, gotBlock, gotAfter)
	}
}
