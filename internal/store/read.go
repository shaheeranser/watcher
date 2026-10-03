package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// occurrenceWindow caps how many recent occurrences a detail response carries.
const occurrenceWindow = 50

// listColumns is shared by List and Get so both select the incident row plus its
// latest explanation the same way.
const listColumns = `
	SELECT i.id, i.fingerprint, i.kind, i.source, COALESCE(i.severity, ''), i.state, i.count,
	       i.first_seen, i.last_seen, i.resolved_at,
	       COALESCE(e.summary, ''), COALESCE(e.error, ''), (e.id IS NOT NULL)
	FROM incidents i
	LEFT JOIN explanations e
	  ON e.id = (SELECT MAX(id) FROM explanations WHERE incident_id = i.id)`

// List returns every stored incident, most recently active first.
func (s *Store) List(ctx context.Context) ([]Incident, error) {
	rows, err := s.ro.QueryContext(ctx, listColumns+` ORDER BY i.last_seen DESC, i.id`)
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	defer rows.Close()

	var out []Incident
	for rows.Next() {
		inc, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

// Get returns one incident's detail, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id string) (Detail, error) {
	row := s.ro.QueryRowContext(ctx, listColumns+` WHERE i.id = ?`, id)
	inc, err := scanIncident(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}

	detail := Detail{Incident: inc}
	latest, has, err := s.latestExplanation(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	if has {
		detail.Latest = &latest
	}
	seen, err := s.recentOccurrences(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	detail.Occurrences = seen
	return detail, nil
}

// Stats counts stored incidents for the health endpoint.
func (s *Store) Stats() Stats {
	var st Stats
	row := s.ro.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN state = 'resolved' THEN 0 ELSE 1 END), 0),
		       COALESCE(SUM(CASE WHEN state = 'resolved' THEN 1 ELSE 0 END), 0)
		FROM incidents`)
	if err := row.Scan(&st.Total, &st.Active, &st.Resolved); err != nil {
		s.log.Error("read stats failed", "error", err)
	}
	return st
}

func (s *Store) latestExplanation(ctx context.Context, id string) (Explanation, bool, error) {
	var (
		ex        Explanation
		createdAt string
		evidence  string
		pending   int
	)
	err := s.ro.QueryRowContext(ctx, `
		SELECT created_at, model, COALESCE(summary, ''), COALESCE(likely_cause, ''),
		       COALESCE(evidence_json, '[]'), COALESCE(suggested_fix, ''),
		       COALESCE(confidence, 0), COALESCE(severity, ''), pending, COALESCE(error, '')
		FROM explanations WHERE incident_id = ? ORDER BY id DESC LIMIT 1`, id).
		Scan(&createdAt, &ex.Model, &ex.Summary, &ex.LikelyCause, &evidence,
			&ex.SuggestedFix, &ex.Confidence, &ex.Severity, &pending, &ex.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return Explanation{}, false, nil
	}
	if err != nil {
		return Explanation{}, false, fmt.Errorf("read explanation: %w", err)
	}
	ex.Pending = pending != 0
	if err := json.Unmarshal([]byte(evidence), &ex.Evidence); err != nil {
		ex.Evidence = nil
	}
	return ex, true, nil
}

func (s *Store) recentOccurrences(ctx context.Context, id string) ([]time.Time, error) {
	rows, err := s.ro.QueryContext(ctx,
		`SELECT seen_at FROM occurrences WHERE incident_id = ? ORDER BY seen_at DESC LIMIT ?`,
		id, occurrenceWindow)
	if err != nil {
		return nil, fmt.Errorf("read occurrences: %w", err)
	}
	defer rows.Close()

	var out []time.Time
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan occurrence: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanIncident(row scanner) (Incident, error) {
	var (
		inc        Incident
		firstSeen  string
		lastSeen   string
		resolvedAt sql.NullString
		hasExpl    bool
	)
	if err := row.Scan(&inc.ID, &inc.Fingerprint, &inc.Kind, &inc.Source, &inc.Severity,
		&inc.State, &inc.Count, &firstSeen, &lastSeen, &resolvedAt,
		&inc.Summary, &inc.ExplanationError, &hasExpl); err != nil {
		return Incident{}, err
	}
	inc.FirstSeen = parseStampTime(firstSeen)
	inc.LastSeen = parseStampTime(lastSeen)
	if resolvedAt.Valid {
		inc.ResolvedAt = parseStampTime(resolvedAt.String)
	}
	inc.HasExplanation = hasExpl
	return inc, nil
}

// parseStampTime reads an RFC3339 UTC timestamp, yielding the zero time for a
// value that does not parse so one malformed row cannot fail a whole list.
func parseStampTime(raw string) time.Time {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}
