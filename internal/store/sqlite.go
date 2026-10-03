package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// defaultQueue bounds the pending-write channel. A full queue drops writes and
// counts them rather than blocking the engine; because an Upsert carries the
// full incident state, the next occurrence repairs a dropped one.
const defaultQueue = 1024

// trimInterval is how often the retention policy runs. Trimming is cheap and
// only needs to keep pace with growth, not with traffic.
const trimInterval = 10 * time.Minute

// writeTimeout bounds one committed mutation so a wedged disk cannot pin the
// writer goroutine forever.
const writeTimeout = 5 * time.Second

// Options configures a SQLite-backed store.
type Options struct {
	Path string

	// Retention removes incidents not seen for this long; zero disables the age
	// cap. OccurrenceCap keeps at most this many occurrence rows per incident;
	// zero disables the cap (DASH-25).
	Retention     time.Duration
	OccurrenceCap int

	Queue  int
	Logger *slog.Logger
}

// Store is the SQLite-backed Backend. Writes go to a single connection on one
// goroutine; reads use a separate query-only pool, so a slow reader cannot block
// the writer and vice versa (DASH-NFR-3, T-2.6).
type Store struct {
	db *sql.DB
	ro *sql.DB

	log           *slog.Logger
	writes        chan op
	events        *broker
	retention     time.Duration
	occurrenceCap int
	dropped       atomic.Int64
}

type opKind int

const (
	opObserve opKind = iota
	opExplain
	opResolve
)

type op struct {
	kind    opKind
	upsert  Upsert
	explain Explain
	resolve Resolve
}

// Open opens or creates the database at opts.Path and migrates it.
func Open(opts Options) (*Store, error) {
	if opts.Path == "" {
		return nil, errors.New("store: empty database path")
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	queue := opts.Queue
	if queue <= 0 {
		queue = defaultQueue
	}

	// The writer holds the only connection to this pool, so writes serialize.
	// WAL lets the read pool run concurrently with them.
	dsn := "file:" + opts.Path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", opts.Path, err)
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	// Handlers must never write, so the read pool is query-only. It is a
	// separate pool because a shared one would serialize reads behind the
	// writer's single connection.
	ro, err := sql.Open("sqlite", "file:"+opts.Path+"?_pragma=busy_timeout(5000)&_pragma=query_only(1)")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open read connection %s: %w", opts.Path, err)
	}
	ro.SetMaxOpenConns(4)
	ro.SetConnMaxLifetime(0)

	return &Store{
		db:            db,
		ro:            ro,
		log:           log,
		writes:        make(chan op, queue),
		events:        newBroker(),
		retention:     opts.Retention,
		occurrenceCap: opts.OccurrenceCap,
	}, nil
}

// Run consumes queued mutations and applies the retention policy until ctx is
// cancelled. It must be started exactly once.
func (s *Store) Run(ctx context.Context) {
	s.trim(ctx)

	t := time.NewTicker(trimInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case o := <-s.writes:
			s.apply(o)
		case <-t.C:
			s.trim(ctx)
		}
	}
}

// Close releases both connections. Callers should cancel Run first.
func (s *Store) Close() error {
	var errs []error
	if s.events != nil {
		s.events.close()
	}
	if s.ro != nil {
		errs = append(errs, s.ro.Close())
	}
	if s.db != nil {
		errs = append(errs, s.db.Close())
	}
	return errors.Join(errs...)
}

// Persistent reports that this backend writes to disk.
func (s *Store) Persistent() bool { return true }

// Dropped reports how many mutations were discarded because the writer was
// behind, for the heartbeat and diagnostics.
func (s *Store) Dropped() int64 { return s.dropped.Load() }

// Subscribe returns an event stream and its cancel function.
func (s *Store) Subscribe() (<-chan Event, func()) { return s.events.subscribe() }

// Observe records one occurrence. It never blocks: a full queue is a dropped
// write, not a stalled pipeline (DASH-NFR-2).
func (s *Store) Observe(u Upsert) { s.enqueue(op{kind: opObserve, upsert: u}) }

// Explain records one explanation attempt.
func (s *Store) Explain(e Explain) { s.enqueue(op{kind: opExplain, explain: e}) }

// Resolve marks an incident resolved.
func (s *Store) Resolve(r Resolve) { s.enqueue(op{kind: opResolve, resolve: r}) }

func (s *Store) enqueue(o op) {
	select {
	case s.writes <- o:
	default:
		if n := s.dropped.Add(1); n == 1 || n%100 == 0 {
			s.log.Warn("dropping persistence write: writer is behind", "dropped", n)
		}
	}
}

func (s *Store) apply(o op) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	switch o.kind {
	case opObserve:
		s.applyObserve(ctx, o.upsert)
	case opExplain:
		s.applyExplain(ctx, o.explain)
	case opResolve:
		s.applyResolve(ctx, o.resolve)
	}
}

func (s *Store) applyObserve(ctx context.Context, u Upsert) {
	inc := u.Incident
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO incidents (id, fingerprint, kind, source, severity, state, count, first_seen, last_seen, resolved_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			kind        = excluded.kind,
			source      = excluded.source,
			severity    = COALESCE(NULLIF(excluded.severity, ''), incidents.severity),
			state       = excluded.state,
			count       = excluded.count,
			first_seen  = excluded.first_seen,
			last_seen   = excluded.last_seen,
			resolved_at = excluded.resolved_at`,
		inc.ID, inc.Fingerprint, inc.Kind, inc.Source, inc.Severity, string(inc.State),
		inc.Count, stamp(inc.FirstSeen), stamp(inc.LastSeen), nullStamp(inc.ResolvedAt))
	if err != nil {
		s.log.Error("persist incident failed", "id", inc.ID, "error", err)
		return
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO occurrences (incident_id, seen_at) VALUES (?, ?)`,
		inc.ID, stamp(u.OccurredAt)); err != nil {
		s.log.Error("persist occurrence failed", "id", inc.ID, "error", err)
	}

	kind := EventCounted
	if inc.State == StateNew {
		kind = EventCreated
	}
	s.events.publish(Event{
		Type: kind, ID: inc.ID, Fingerprint: inc.Fingerprint, Source: inc.Source,
		Count: inc.Count, State: string(inc.State), Fields: []string{"count", "last_seen"},
	})
}

func (s *Store) applyExplain(ctx context.Context, e Explain) {
	ex := e.Explanation
	evidence := "[]"
	if len(ex.Evidence) > 0 {
		if b, err := marshalEvidence(ex.Evidence); err == nil {
			evidence = b
		}
	}
	pending := 0
	if ex.Pending {
		pending = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO explanations (incident_id, created_at, model, summary, likely_cause, evidence_json, suggested_fix, confidence, severity, pending, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, stamp(e.At), ex.Model, ex.Summary, ex.LikelyCause, evidence,
		ex.SuggestedFix, ex.Confidence, ex.Severity, pending, ex.Error)
	if err != nil {
		s.log.Error("persist explanation failed", "id", e.ID, "error", err)
		return
	}
	if ex.Severity != "" && ex.Error == "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE incidents SET severity = ? WHERE id = ?`, ex.Severity, e.ID); err != nil {
			s.log.Error("persist severity failed", "id", e.ID, "error", err)
		}
	}
	s.events.publish(Event{
		Type: EventExplained, ID: e.ID, Fingerprint: e.Fingerprint,
		Fields: []string{"summary", "likely_cause", "evidence", "suggested_fix", "confidence", "severity"},
	})
}

func (s *Store) applyResolve(ctx context.Context, r Resolve) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE incidents SET state = ?, resolved_at = ?, count = ? WHERE id = ?`,
		string(StateResolved), stamp(r.ResolvedAt), r.Count, r.ID)
	if err != nil {
		s.log.Error("persist resolution failed", "id", r.ID, "error", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	s.events.publish(Event{
		Type: EventStateChanged, ID: r.ID, Count: r.Count, State: string(StateResolved),
		Fields: []string{"state", "resolved_at"},
	})
}

// stamp renders a time in the RFC3339 UTC form the JSONL contract already uses,
// so the same value round-trips without a conversion surprise.
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// nullStamp renders the zero time as SQL NULL, so a reopened incident clears its
// previous resolved_at rather than carrying a stale one.
func nullStamp(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return stamp(t)
}

func marshalEvidence(lines []string) (string, error) {
	b, err := json.Marshal(lines)
	return string(b), err
}
