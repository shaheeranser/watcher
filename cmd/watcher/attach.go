package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shaheeranser/watcher/internal/api"
	"github.com/shaheeranser/watcher/internal/config"
	"github.com/shaheeranser/watcher/internal/tui"
)

const attachUsage = `watcher attach renders a running daemon's incident dashboard.

Usage:
  watcher attach [flags]

Flags (environment variable in parentheses):
  --api-socket PATH       unix socket of the running daemon (WATCHER_API_SOCKET; default $XDG_RUNTIME_DIR/watcher.sock)
  --refresh-interval DUR  bound on UI staleness and the polling fallback (WATCHER_REFRESH_INTERVAL; default 2s)
  --list-fraction FLOAT   share of height for the incident list (WATCHER_LIST_FRACTION; default 0.45)

Keys:
  up/down, k/j   select an incident        g / G      first / last row
  tab            switch panes              pgup/pgdn  scroll the detail pane
  r              refresh                   ?          toggle help
  q, ctrl+c      quit (the daemon keeps running)`

// attachCommand connects to a running daemon and renders its dashboard. It is
// read-only: it opens the socket, GETs history and the event stream, and never
// writes incident state (DASH-10).
func attachCommand(args []string) int {
	cfg, err := config.ParseAttach(args, os.Getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, attachUsage)
			return 0
		}
		fmt.Fprintf(os.Stderr, "watcher attach: %v\n", err)
		return 2
	}

	client := api.NewClient(cfg.APISocket)

	// Fail fast with an actionable message when no daemon answers, rather than
	// hanging or showing an empty screen (DASH-8).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	health, err := client.Health(ctx)
	if err != nil {
		cancel()
		fmt.Fprintf(os.Stderr, "watcher attach: no watcher daemon on %s\n  %v\n", cfg.APISocket, err)
		fmt.Fprintln(os.Stderr, "start one with `watcher run` (the API may be disabled with --api=false).")
		return 1
	}
	rows, err := client.Incidents(ctx)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "watcher attach: cannot read incidents from %s: %v\n", cfg.APISocket, err)
		return 1
	}

	degraded := os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"
	model := tui.New(tui.Options{
		Fetcher:            client,
		Initial:            rows,
		ListFraction:       cfg.ListFraction,
		Degraded:           degraded,
		HistoryUnavailable: !health.Persistent,
	})

	program := tea.NewProgram(model, tea.WithAltScreen())

	// The event source runs outside the render loop and pushes messages in, so
	// Update and View stay free of I/O (DASH-NFR-1).
	sourceCtx, cancelSource := context.WithCancel(context.Background())
	defer cancelSource()
	go tui.RunSource(sourceCtx,
		func(ctx context.Context) (tui.EventReader, error) { return client.Events(ctx) },
		cfg.RefreshInterval,
		program.Send,
	)

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "watcher attach: %v\n", err)
		return 1
	}
	return 0
}
