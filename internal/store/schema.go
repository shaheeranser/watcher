package store

import (
	"database/sql"
	"fmt"
)

// schemaVersion is bumped whenever the DDL changes; migrate reads it back to
// decide whether an upgrade step is needed. Version 1 is the initial schema.
const schemaVersion = 1

// schema is idempotent so opening an existing database is a no-op migration.
// Explanations are a table, not a column, because a fingerprint re-explained
// after the window should add history rather than overwrite the first attempt.
const schema = `
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS incidents (
  id           TEXT PRIMARY KEY,
  fingerprint  TEXT NOT NULL,
  kind         TEXT NOT NULL,
  source       TEXT NOT NULL,
  severity     TEXT,
  state        TEXT NOT NULL,
  count        INTEGER NOT NULL DEFAULT 0,
  first_seen   TEXT NOT NULL,
  last_seen    TEXT NOT NULL,
  resolved_at  TEXT
);

CREATE TABLE IF NOT EXISTS explanations (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  incident_id   TEXT NOT NULL REFERENCES incidents(id),
  created_at    TEXT NOT NULL,
  model         TEXT NOT NULL,
  summary       TEXT,
  likely_cause  TEXT,
  evidence_json TEXT,
  suggested_fix TEXT,
  confidence    REAL,
  severity      TEXT,
  pending       INTEGER NOT NULL DEFAULT 0,
  error         TEXT
);

CREATE TABLE IF NOT EXISTS occurrences (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  incident_id TEXT NOT NULL REFERENCES incidents(id),
  seen_at     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_occ_incident_time
  ON occurrences (incident_id, seen_at DESC);

CREATE INDEX IF NOT EXISTS idx_incidents_state_last
  ON incidents (state, last_seen DESC);
`

// migrate creates the schema and records its version. It is safe to run against
// an empty file and against a database this version already created.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	var version int
	err := db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&version)
	switch {
	case err == sql.ErrNoRows:
		if _, err := db.Exec(`INSERT INTO schema_version (version) VALUES (?)`, schemaVersion); err != nil {
			return fmt.Errorf("record schema version: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("read schema version: %w", err)
	case version > schemaVersion:
		return fmt.Errorf("database schema version %d is newer than this binary supports (%d)", version, schemaVersion)
	}
	// version <= schemaVersion: the current schema is already in place. An older
	// version would run an upgrade step here; there is none before version 1.
	return nil
}
