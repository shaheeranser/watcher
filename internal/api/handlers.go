package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/shaheeranser/watcher/internal/store"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	stats := s.reader.Stats()
	writeJSON(w, http.StatusOK, Health{
		Status:           "ok",
		Version:          s.version,
		APIVersion:       Version,
		UptimeSeconds:    int64(time.Since(s.started).Seconds()),
		IncidentsTracked: stats.Total,
		Active:           stats.Active,
		Resolved:         stats.Resolved,
		Persistent:       s.reader.Persistent(),
	})
}

func (s *Server) handleIncidents(w http.ResponseWriter, r *http.Request) {
	list, err := s.reader.List(r.Context())
	if err != nil {
		s.log.Error("list incidents failed", "error", err)
		writeError(w, http.StatusInternalServerError, "cannot read incidents")
		return
	}
	rows := make([]IncidentRow, 0, len(list))
	for _, inc := range list {
		rows = append(rows, rowOf(inc))
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleIncidentDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusNotFound, "unknown incident")
		return
	}
	detail, err := s.reader.Get(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "unknown incident")
	case err != nil:
		s.log.Error("read incident failed", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "cannot read incident")
	default:
		writeJSON(w, http.StatusOK, detailOf(detail))
	}
}

// handleEvents streams per-mutation notices as Server-Sent Events. It is
// deliberately one-way and event-per-mutation, so an attach that nobody is
// watching costs nothing and a stalled reader is dropped rather than allowed to
// block the daemon (DASH-19, DASH-21, DASH-NFR-3).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.events == nil {
		writeError(w, http.StatusServiceUnavailable, "event stream unavailable")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	events, cancel := s.events.Subscribe()
	defer cancel()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	keepalive := time.NewTicker(sseKeepalive)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
