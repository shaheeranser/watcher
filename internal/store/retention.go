package store

import (
	"context"
	"time"
)

// trim applies the retention policy: it drops occurrence rows beyond the
// per-incident cap and whole incidents older than the retention age. Occurrences
// are the growth chokepoint, so capping them keeps the database bounded while
// incidents and their explanations stay small (DASH-25, design §3.2).
func (s *Store) trim(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if s.occurrenceCap > 0 {
		res, err := s.db.ExecContext(ctx, `
			DELETE FROM occurrences
			WHERE id IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (
						PARTITION BY incident_id ORDER BY seen_at DESC, id DESC
					) AS rn
					FROM occurrences
				) WHERE rn > ?)`, s.occurrenceCap)
		switch {
		case err != nil:
			s.log.Error("occurrence retention failed", "error", err)
		default:
			if n, _ := res.RowsAffected(); n > 0 {
				s.log.Debug("trimmed old occurrences", "removed", n)
			}
		}
	}

	if s.retention > 0 {
		cutoff := stamp(time.Now().Add(-s.retention))
		var removed int64
		for _, stmt := range []string{
			`DELETE FROM occurrences WHERE incident_id IN (SELECT id FROM incidents WHERE last_seen < ?)`,
			`DELETE FROM explanations WHERE incident_id IN (SELECT id FROM incidents WHERE last_seen < ?)`,
			`DELETE FROM incidents WHERE last_seen < ?`,
		} {
			res, err := s.db.ExecContext(ctx, stmt, cutoff)
			if err != nil {
				s.log.Error("retention failed", "error", err)
				return
			}
			if n, _ := res.RowsAffected(); n > removed {
				removed = n
			}
		}
		if removed > 0 {
			s.log.Debug("trimmed aged incidents", "removed", removed)
		}
	}
}
