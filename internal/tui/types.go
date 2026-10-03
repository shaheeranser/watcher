// Package tui renders the live dashboard `watcher attach` shows. It owns no
// state beyond what it displays: rendering and Update never perform I/O, so
// input latency is independent of API and database latency (DASH-NFR-1). Fetch
// work happens only in tea.Cmd closures, which bubbletea runs off the render
// loop.
package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shaheeranser/watcher/internal/api"
	"github.com/shaheeranser/watcher/internal/store"
)

const (
	// fetchTimeout bounds one list or detail request so a wedged daemon cannot
	// leave a command hanging forever.
	fetchTimeout = 3 * time.Second

	// minWidth and minHeight are the smallest terminal the two-pane layout is
	// rendered in; below them a single message is shown instead (DASH-16).
	minWidth  = 60
	minHeight = 16
)

// Fetcher is the read-only client the model uses. It is an interface so tests
// can drive the model without a daemon.
type Fetcher interface {
	Incidents(ctx context.Context) ([]api.IncidentRow, error)
	Incident(ctx context.Context, id string) (api.IncidentDetail, error)
}

// EventReader is one live Server-Sent Events connection.
type EventReader interface {
	Next() (store.Event, error)
	Close() error
}

// OpenFunc opens the event stream; the TUI calls it again after a disconnect to
// reconnect with backoff (DASH-9).
type OpenFunc func(ctx context.Context) (EventReader, error)

// ConnState is the daemon-connection state shown in the footer.
type ConnState int

const (
	ConnConnected ConnState = iota
	ConnReconnecting
)

func (c ConnState) String() string {
	if c == ConnReconnecting {
		return "reconnecting"
	}
	return "connected"
}

// Focus is which pane receives navigation keys.
type Focus int

const (
	FocusList Focus = iota
	FocusDetail
)

// rowsMsg replaces the incident list.
type rowsMsg struct {
	rows []api.IncidentRow
	err  error
}

// detailMsg carries a lazily fetched detail for one incident.
type detailMsg struct {
	id     string
	detail api.IncidentDetail
	err    error
}

// eventMsg announces a mutation from the event stream. It carries the event so
// the model can tell whether the currently selected incident changed and must be
// refetched, rather than refetching every selected detail on every event.
type eventMsg struct {
	event store.Event
}

// pollMsg is the watchdog's fallback, sent when the stream has gone quiet so the
// UI still updates (DASH-21).
type pollMsg struct{}

// connMsg reports a connection state change.
type connMsg struct {
	state ConnState
	err   error
}

// sendFunc delivers a message into the running bubbletea program.
type sendFunc func(tea.Msg)
