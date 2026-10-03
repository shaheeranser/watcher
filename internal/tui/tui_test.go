package tui

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shaheeranser/watcher/internal/api"
	"github.com/shaheeranser/watcher/internal/store"
)

var update = flag.Bool("update", false, "update golden files")

var fixedNow = time.Date(2026, 1, 2, 15, 6, 13, 0, time.UTC)

func sampleRows() []api.IncidentRow {
	return []api.IncidentRow{
		{
			ID: "web-1:aaaa", Fingerprint: "aaaa", Kind: "go-panic", Source: "web-1",
			Severity: "high", State: "ongoing", Count: 12,
			FirstSeen: "2026-01-02T15:04:05Z", LastSeen: "2026-01-02T15:06:11Z",
			Summary: "panic: send on closed channel",
		},
		{
			ID: "worker-2:bbbb", Fingerprint: "bbbb", Kind: "python-traceback", Source: "worker-2",
			Severity: "medium", State: "ongoing", Count: 3,
			FirstSeen: "2026-01-02T15:05:00Z", LastSeen: "2026-01-02T15:05:12Z",
			Summary: "Traceback: KeyError 'user_id'",
		},
		{
			ID: "api-1:cccc", Fingerprint: "cccc", Kind: "generic-fatal", Source: "api-1",
			Severity: "medium", State: "resolved", Count: 1,
			FirstSeen: "2026-01-02T14:00:00Z", LastSeen: "2026-01-02T14:00:00Z",
			Summary: "FATAL: connection refused",
		},
		{
			ID: "api-1:dddd", Fingerprint: "dddd", Kind: "generic-fatal", Source: "api-1",
			State: "ongoing", Count: 1,
			FirstSeen: "2026-01-02T15:06:12Z", LastSeen: "2026-01-02T15:06:12Z",
			ExplanationPending: true,
		},
	}
}

type fakeFetcher struct {
	rows    []api.IncidentRow
	details map[string]api.IncidentDetail
	err     error
}

func (f *fakeFetcher) Incidents(context.Context) ([]api.IncidentRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *fakeFetcher) Incident(_ context.Context, id string) (api.IncidentDetail, error) {
	d, ok := f.details[id]
	if !ok {
		return api.IncidentDetail{}, errors.New("not found")
	}
	return d, nil
}

func keyMsg(s string) tea.Msg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func sized(rows []api.IncidentRow, w, h int) Model {
	m := New(Options{
		Fetcher:  &fakeFetcher{rows: rows},
		Initial:  rows,
		Degraded: true,
		Now:      func() time.Time { return fixedNow },
	})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(Model)
}

func TestViewAtManySizesDoesNotPanic(t *testing.T) {
	sizes := []struct{ w, h int }{
		{0, 0}, {1, 1}, {20, 5}, {59, 15}, {60, 16}, {80, 24}, {120, 40}, {200, 8}, {40, 60},
	}
	for _, size := range sizes {
		m := sized(sampleRows(), size.w, size.h)
		got := m.View()
		if got == "" {
			t.Fatalf("%dx%d: empty view", size.w, size.h)
		}
		if size.w < minWidth || size.h < minHeight {
			if !strings.Contains(got, "too small") {
				t.Errorf("%dx%d: expected the too-small message, got %q", size.w, size.h, got)
			}
		}
	}
}

func TestKeyDrivenSelectionAndFocus(t *testing.T) {
	m := sized(sampleRows(), 100, 30)

	// Sorted: two unresolved newest-first (dddd, aaaa), then bbbb (older), then
	// the resolved cccc.
	if m.selectedID != "api-1:dddd" {
		t.Fatalf("initial selection = %q, want the most recent unresolved api-1:dddd", m.selectedID)
	}

	updated, cmd := m.Update(keyMsg("down"))
	m = updated.(Model)
	if m.selectedID != "web-1:aaaa" {
		t.Errorf("after down selection = %q, want web-1:aaaa", m.selectedID)
	}
	if cmd == nil {
		t.Error("moving selection should fetch the new detail")
	}

	m = press(m, "j")
	if m.selectedID != "worker-2:bbbb" {
		t.Errorf("after j selection = %q, want worker-2:bbbb", m.selectedID)
	}
	m = press(m, "G")
	if m.selectedID != "api-1:cccc" {
		t.Errorf("after G selection = %q, want last row", m.selectedID)
	}
	m = press(m, "g")
	if m.selectedID != "api-1:dddd" {
		t.Errorf("after g selection = %q, want first row", m.selectedID)
	}

	if m.focus != FocusList {
		t.Fatal("focus should start on the list")
	}
	m = press(m, "tab")
	if m.focus != FocusDetail {
		t.Error("tab should switch focus to the detail pane")
	}
	m = press(m, "shift+tab")
	if m.focus != FocusList {
		t.Error("shift+tab should switch focus back")
	}
}

func press(m Model, key string) Model {
	updated, _ := m.Update(keyMsg(key))
	return updated.(Model)
}

func TestHelpOverlayToggles(t *testing.T) {
	m := sized(sampleRows(), 100, 30)
	m = press(m, "?")
	if !m.help {
		t.Fatal("? should open help")
	}
	if !strings.Contains(m.View(), "toggle this help") {
		t.Errorf("help view missing bindings: %q", m.View())
	}
	m = press(m, "esc")
	if m.help {
		t.Fatal("esc should close help")
	}
}

func TestSortOrder(t *testing.T) {
	got := sortRows(sampleRows())
	want := []string{"api-1:dddd", "web-1:aaaa", "worker-2:bbbb", "api-1:cccc"}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("row %d = %q, want %q (order: %v)", i, got[i].ID, id, ids(got))
		}
	}
}

func TestReselectKeepsSelectionAcrossRefresh(t *testing.T) {
	m := sized(sampleRows(), 100, 30)
	m = press(m, "j") // select web-1:aaaa
	before := m.selectedID

	// A refresh that reorders rows must keep the same incident selected.
	reordered := []api.IncidentRow{sampleRows()[1], sampleRows()[0], sampleRows()[2], sampleRows()[3]}
	updated, _ := m.Update(rowsMsg{rows: reordered})
	m = updated.(Model)
	if m.selectedID != before {
		t.Errorf("selection = %q, want %q after refresh", m.selectedID, before)
	}
}

func TestPendingAndUnavailableMarkers(t *testing.T) {
	row := api.IncidentRow{ID: "x:1", State: "ongoing", ExplanationPending: true}
	m := sized([]api.IncidentRow{row}, 100, 30)
	if !strings.Contains(m.View(), "analysing") {
		t.Errorf("pending row should show an analysing marker:\n%s", m.View())
	}

	row = api.IncidentRow{ID: "y:2", State: "ongoing", ExplanationUnavailable: true}
	m = sized([]api.IncidentRow{row}, 100, 30)
	if !strings.Contains(m.View(), "explanation unavailable") {
		t.Errorf("failed row should show an unavailable marker:\n%s", m.View())
	}
}

func TestDetailRendersExplanation(t *testing.T) {
	detail := api.IncidentDetail{
		IncidentRow: api.IncidentRow{
			ID: "web-1:aaaa", Fingerprint: "a1b2c3d4e5f6a7b8", Kind: "go-panic", Source: "web-1",
			Severity: "high", State: "ongoing", Count: 12,
			FirstSeen: "2026-01-02T15:04:05Z", LastSeen: "2026-01-02T15:06:11Z",
			Summary: "panic: send on closed channel",
		},
		LikelyCause:  "the hub closed before Broadcast ran",
		Evidence:     []string{"panic: send on closed channel", "main.(*Hub).Broadcast(...)"},
		SuggestedFix: "select on a done channel before sending",
		Confidence:   0.72,
		Occurrences:  []string{"2026-01-02T15:06:11Z", "2026-01-02T15:04:05Z"},
	}
	m := sized(sampleRows(), 100, 30)
	updated, _ := m.Update(detailMsg{id: "api-1:dddd", detail: detail})
	m = updated.(Model)
	view := m.View()
	for _, want := range []string{"Likely cause", "the hub closed before Broadcast ran", "Evidence", "Suggested fix", "0.72", "Count 12"} {
		if !strings.Contains(view, want) {
			t.Errorf("detail view missing %q:\n%s", want, view)
		}
	}
}

func TestGoldenDashboard(t *testing.T) {
	rows := sortRows(sampleRows())
	selected := rows[1] // web-1:aaaa, an explained incident

	m := New(Options{
		Fetcher:  &fakeFetcher{},
		Degraded: true,
		Now:      func() time.Time { return fixedNow },
	})
	m.width, m.height = 100, 24
	m.rows = rows
	m.selected = 1
	m.selectedID = selected.ID
	m.detailID = selected.ID
	m.detail = &api.IncidentDetail{
		IncidentRow:  selected,
		LikelyCause:  "the hub closed before Broadcast ran",
		Evidence:     []string{"panic: send on closed channel", "main.(*Hub).Broadcast(...)"},
		SuggestedFix: "select on a done channel before sending",
		Confidence:   0.72,
		Occurrences:  []string{"2026-01-02T15:06:11Z", "2026-01-02T15:04:05Z"},
	}

	got := m.View() + "\n"
	path := filepath.Join("testdata", "dashboard.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func ids(rows []api.IncidentRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// --- source tests ---

type streamResult struct {
	ev  store.Event
	err error
}

type fakeReader struct {
	ch        chan streamResult
	done      chan struct{}
	errOnNext error
	once      sync.Once
}

func newFakeReader() *fakeReader {
	return &fakeReader{ch: make(chan streamResult, 8), done: make(chan struct{})}
}

func (f *fakeReader) Next() (store.Event, error) {
	if f.errOnNext != nil {
		return store.Event{}, f.errOnNext
	}
	select {
	case r := <-f.ch:
		return r.ev, r.err
	case <-f.done:
		return store.Event{}, io.EOF
	}
}

func (f *fakeReader) Close() error {
	f.once.Do(func() { close(f.done) })
	return nil
}

func TestRunSourceForwardsEvents(t *testing.T) {
	reader := newFakeReader()
	send := make(chan tea.Msg, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go RunSource(ctx, func(context.Context) (EventReader, error) { return reader, nil }, 2*time.Second, func(m tea.Msg) { send <- m })

	if _, ok := waitForMsg[connMsg](t, send, time.Second); !ok {
		t.Fatal("expected an initial connected message")
	}
	reader.ch <- streamResult{ev: store.Event{Type: store.EventCreated, ID: "x"}}
	if msg, ok := waitForMsg[eventMsg](t, send, time.Second); !ok {
		t.Fatalf("expected an event message, got %T", msg)
	}
}

func TestRunSourcePollsWhenQuiet(t *testing.T) {
	reader := newFakeReader()
	send := make(chan tea.Msg, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A 200ms refresh yields a 1s watchdog.
	go RunSource(ctx, func(context.Context) (EventReader, error) { return reader, nil }, 200*time.Millisecond, func(m tea.Msg) { send <- m })

	if _, ok := waitForMsg[connMsg](t, send, time.Second); !ok {
		t.Fatal("expected an initial connected message")
	}
	if _, ok := waitForMsg[pollMsg](t, send, 3*time.Second); !ok {
		t.Fatal("expected the watchdog to trigger a poll while the stream was quiet")
	}
}

func TestRunSourceReconnectsAfterDisconnect(t *testing.T) {
	var opens int32
	var mu sync.Mutex
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	send := make(chan tea.Msg, 32)

	open := func(context.Context) (EventReader, error) {
		mu.Lock()
		opens++
		mu.Unlock()
		return &fakeReader{ch: make(chan streamResult), done: make(chan struct{}), errOnNext: io.EOF}, nil
	}
	go RunSource(ctx, open, 200*time.Millisecond, func(m tea.Msg) { send <- m })

	// First: connected, then reconnecting after the stream ends, then connected
	// again once it reopens.
	var states []ConnState
	deadline := time.After(4 * time.Second)
	for len(states) < 3 {
		select {
		case m := <-send:
			if cm, ok := m.(connMsg); ok {
				states = append(states, cm.state)
			}
		case <-deadline:
			t.Fatalf("states seen = %v, want connected/reconnecting/connected", states)
		}
	}
	if states[0] != ConnConnected || states[1] != ConnReconnecting || states[2] != ConnConnected {
		t.Fatalf("states = %v", states)
	}
}

func waitForMsg[T any](t *testing.T, ch <-chan tea.Msg, timeout time.Duration) (T, bool) {
	t.Helper()
	var zero T
	deadline := time.After(timeout)
	for {
		select {
		case m := <-ch:
			if typed, ok := m.(T); ok {
				return typed, true
			}
		case <-deadline:
			return zero, false
		}
	}
}
