package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/shaheeranser/watcher/internal/store"
)

// sseKeepalive is how often an idle event stream sends a comment line. It keeps
// proxies and the client connection alive; the client's own watchdog decides
// when to fall back to polling (DASH-21).
const sseKeepalive = 15 * time.Second

// shutdownGrace bounds how long a listener waits for in-flight requests.
const shutdownGrace = 3 * time.Second

// EventSource is the daemon's mutation stream, satisfied by store.Backend.
type EventSource interface {
	Subscribe() (<-chan store.Event, func())
}

// Options configures the read API.
type Options struct {
	Socket  string
	Reader  store.Reader
	Events  EventSource
	Logger  *slog.Logger
	Version string
	Started time.Time
}

// Server serves the versioned read API. It holds no mutable state of its own:
// every response is projected from the store, so a client can attach and detach
// without the daemon doing any per-client work beyond the request itself
// (DASH-3).
type Server struct {
	socket  string
	reader  store.Reader
	events  EventSource
	log     *slog.Logger
	version string
	started time.Time
	handler http.Handler
}

// New builds the server and its routing table.
func New(opts Options) *Server {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	started := opts.Started
	if started.IsZero() {
		started = time.Now()
	}
	s := &Server{
		socket:  opts.Socket,
		reader:  opts.Reader,
		events:  opts.Events,
		log:     log,
		version: version,
		started: started,
	}
	s.handler = s.routes()
	return s
}

// Handler exposes the routing table so tests can drive it with httptest.
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+Prefix+"/health", s.handleHealth)
	mux.HandleFunc("GET "+Prefix+"/incidents", s.handleIncidents)
	mux.HandleFunc("GET "+Prefix+"/incidents/{id...}", s.handleIncidentDetail)
	mux.HandleFunc("GET "+Prefix+"/events", s.handleEvents)
	return mux
}

// Listen binds the Unix socket with 0600 permissions. It fails clearly when a
// live daemon already holds the path rather than binding somewhere else
// (DASH-5).
func (s *Server) Listen() (net.Listener, error) {
	return listenUnix(s.socket)
}

// Serve serves until ctx is cancelled or the listener fails. The caller owns
// the listener so a bind failure is reported before the daemon starts serving.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	srv := &http.Server{Handler: s.handler}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(l) }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			s.log.Warn("api shutdown incomplete", "error", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already sent, so the only recourse is to log it.
		_ = err
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errBody{Error: msg})
}
