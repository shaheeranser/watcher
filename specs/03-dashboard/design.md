# 03 — Dashboard: Design

Requirements: [`requirements.md`](./requirements.md). Tasks:
[`tasks.md`](./tasks.md).

Dependencies: builds on `../01-core-engine/design.md` (pipeline, `Result`
type, config precedence) and `../02-production-shape/design.md` (incident
state machine, notification kinds, container sources). It consumes the
tracker's outputs; it does not change detection, fingerprinting, context
curation, or the backend.

## 1. The central design decision: always headless

The daemon must never depend on a terminal. Reasons:

1. **Push, not pull.** A daemon that renders a TUI is a daemon someone has to
   be watching. The product's premise is that nobody is watching until
   something goes wrong.
2. **Containers have no TTY.** In the Compose deployment, the daemon runs as a
   service with no terminal; a UI built into the daemon would be dead code in
   production and would tempt split-brain state.

So the boundary is: **the daemon owns state and serves it; `attach` owns
rendering and owns nothing else.** `attach` is a separate process launched
explicitly by an operator, and it can be killed and restarted at will without
affecting detection.

```
                 ┌───────────────────────────────┐
   logs ────────▶│  watcher daemon (headless)    │
                 │   pipeline → tracker → state  │──── SQLite (history)
                 │                    machine    │
                 │                        │      │
                 │        read API ◀──────┘      │
                 └───────────┬───────────────────┘
                             │ unix socket / loopback HTTP
                             │ (HTTP JSON + event stream)
                 ┌───────────▼───────────────────┐
                 │  watcher attach (TUI client)  │
                 │  bubbletea + lipgloss         │
                 └───────────────────────────────┘
```

Because the socket is a normal transport, this also makes the API usable by
other tooling later without designing a plugin surface now.

## 2. Transport and API surface

### 2.1 Transport

- Default: a Unix domain socket at `${XDG_RUNTIME_DIR}/watcher.sock`, falling
  back to `/var/run/watcher.sock`.
- The socket is created with `0600` permissions, owned by the daemon's user,
  so access control is filesystem-based and no auth token is needed.
- If the socket path is not writable, the daemon can be configured to listen
  on loopback TCP with a shared-secret header instead — flagged as OD-03-1.
- The daemon fails clearly when the socket path is already held by a live
  process (DASH-5) rather than silently binding an alternative.

### 2.2 Routes (v1)

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/v1/health` | Liveness, version, uptime, counters |
| `GET` | `/api/v1/incidents` | List rows: fingerprint, kind, source, severity, state, count, first_seen, last_seen, summary (truncated), explanation_pending |
| `GET` | `/api/v1/incidents/{id}` | Full detail: the list fields plus explanation fields, evidence array, and recent occurrence timestamps |
| `GET` | `/api/v1/events` | Server-Sent Events stream of mutations (created, counted, state-changed, explained) |

`{id}` is the fingerprint hash — it is stable across restarts, which is what
makes the persistence and the "one row per distinct crash" goal line up.

### 2.3 Live updates

Two mechanisms, deliberately redundant (DASH-19, DASH-21):

- **SSE** (`/api/v1/events`) is the primary channel: the daemon pushes a small
  event per mutation. SSE avoids a full WebSocket dependency, is trivially
  proxyable, and is a one-way stream, which matches the read-only UI.
- **Polling** is the fallback: if no SSE event arrives within a watchdog
  interval, the client re-fetches `/api/v1/incidents`. This guarantees the UI
  never silently freezes if the stream stalls behind a proxy.

Events carry the fingerprint and the changed field set, not the full row; the
client fetches detail lazily for the selected incident. This keeps the stream
small and means an unattended attach cannot balloon memory.

## 3. Persistence

### 3.1 Driver

A pure-Go SQLite driver (e.g. `modernc.org/sqlite`) is chosen over a cgo driver
to preserve the single-static-binary, no-cgo property from
`../01-core-engine/requirements.md` (CORE-NFR-5). Exact dependency is OD-03-2.

### 3.2 Schema

```sql
PRAGMA journal_mode = WAL;   -- concurrent readers alongside the writer

CREATE TABLE IF NOT EXISTS incidents (
  id           TEXT PRIMARY KEY,     -- fingerprint hash
  fingerprint  TEXT NOT NULL,
  kind         TEXT NOT NULL,
  source       TEXT NOT NULL,
  severity     TEXT,                 -- low|medium|high|critical, null until explained
  state        TEXT NOT NULL,        -- new|ongoing|resolved
  count        INTEGER NOT NULL DEFAULT 0,
  first_seen   TEXT NOT NULL,        -- RFC3339 UTC
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
  evidence_json TEXT,                -- JSON array, verbatim lines
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
```

Notes:

- `explanations` is a history, not a single column, so a later re-explanation
  of the same fingerprint (after the explanation window elapses) is recorded
  rather than overwriting the earlier attempt.
- `occurrences` rows are what the detail pane's "recent activity" is built
  from; they are the retention chokepoint (DASH-25) — trimming old occurrence
  rows keeps the DB bounded while incidents and explanations stay small.
- Timestamps are stored as RFC3339 UTC text, matching the JSONL contract from
  milestone 01, so the same values round-trip without conversion surprises.

### 3.3 Write path and isolation

Database writes happen on a single dedicated goroutine consuming a channel, so:

- The detection pipeline never blocks on disk (DASH-NFR-2).
- The tracker's in-memory map remains the source of truth for the live state
  machine; the DB is a projection for history and the UI. This keeps milestone
  02's state machine semantics intact and makes persistence non-load-bearing: if
  the DB is unavailable, detection and notification still work (DASH-26).

WAL mode plus a read-only connection for the API handler gives concurrent
readers without blocking the writer (DASH-NFR-3).

### 3.4 Restart semantics

Two distinct things are persisted, and they matter differently:

- **History** (what happened) — always persisted; the UI shows prior incidents
  after a restart (DASH-23).
- **Notification state** (what we have already told someone) — the milestone-02
  design deliberately kept this process-local, with the rule that a restart
  never emits spurious `resolved` notifications (`../02-production-shape/design.md`
  §4.3).

This milestone does not change that rule: on startup the daemon rehydrates
history for display, but treats fingerprints as fresh for *notification*
purposes. Whether counts should instead resume cumulatively across a restart is
flagged as OD-03-3, because it changes user-visible notification text ("now at
N" where N resets versus accumulates).

## 4. TUI design (`watcher attach`)

### 4.1 Model

A single bubbletea model owning:

- `rows []IncidentRow` — sorted list state (DASH-17).
- `selected int` — cursor.
- `focus` — list or detail.
- `detail *IncidentDetail` — lazily fetched for the selected row.
- `conn` — connected / reconnecting / down (DASH-9).
- `events <-chan Event` — plumbing from the SSE/poll source into `Update`.

Data flow: an SSE (or poll) source goroutine emits `tea.Msg` values into the
program; the model updates state and re-renders. Rendering never performs I/O.
This is what keeps input latency independent of API/DB latency (DASH-NFR-1).

### 4.2 Layout

```
┌ Incidents ──────────────────────────────── 7 active · 2 resolved ─────────┐
│ ● HIGH   go-panic      web-1         ×12   2s ago   panic: send on closed… │
│ ● HIGH   python-tb     worker-2      ×3    1m ago   Traceback: KeyError…   │
│ ○ MED    generic-fatal api-1         ×1    9m ago   FATAL: connection re…  │
│ …                                                                          │
├ Detail ── a1b2c3d4e5f6a7b8 ── ongoing ─────────────────────────────────────┤
│ Summary      …                                                             │
│ Likely cause …                                                             │
│ Evidence     > panic: send on closed channel                               │
│              >   main.(*Hub).Broadcast(...)                                │
│ Suggested fix …                                                            │
│ Confidence   0.72   Severity HIGH   First 15:04:05Z   Last 15:06:11Z       │
├────────────────────────────────────────────────────────────────────────────┤
│ ↑/↓ select · tab panes · r refresh · ? help · q quit        ● connected     │
└────────────────────────────────────────────────────────────────────────────┘
```

- The list pane is allocated a configurable fraction of height (default ~45%),
  the detail pane the remainder; both are `lipgloss`-styled and clipped to
  their widths.
- Sort order (DASH-17): `resolved` last; within a state group, descending
  `last_seen`. Severity is shown per row but does *not* reorder the list, so
  rows do not jump around as severities arrive late — motion is the enemy of a
  list you are reading.
- Pending/unavailable explanations render as an explicit marker
  (`⏳ analysing` / `⚠ explanation unavailable`) rather than empty text
  (DASH-18).
- Resize handling recomputes layout from `WindowSizeMsg` every time (DASH-15);
  below a minimum (e.g. 60×16) the program renders a single centered message
  (DASH-16) instead of a broken two-pane layout.

### 4.3 Keys

| Key | Action |
|-----|--------|
| `↑` / `k`, `↓` / `j` | Move selection |
| `g` / `G` | Jump to first / last row |
| `tab` / `shift+tab` | Switch focus between list and detail |
| `pgup` / `pgdn` | Scroll the detail pane |
| `r` | Force refresh |
| `?` | Toggle help overlay |
| `q`, `ctrl+c` | Quit attach (does not affect the daemon) |

### 4.4 Degradation

`NO_COLOR` and `TERM=dumb` disable styling and switch the status glyphs to
ASCII (`*` / `o`) so the UI stays readable where color and box-drawing are
unavailable (DASH-NFR-4).

## 5. Reconnect and failure behavior

| Situation | Behavior |
|-----------|----------|
| No daemon at startup | Clear message + non-zero exit (DASH-8) |
| Socket disappears mid-session | Show "disconnected", retry with backoff, resume (DASH-9) |
| SSE stalls but socket alive | Watchdog triggers polling fallback (DASH-21) |
| Daemon restarted | Reconnect, re-fetch list, reselect the same fingerprint if it still exists |
| DB unavailable | Daemon runs without persistence; API serves in-memory state, UI shows a "history unavailable" banner (DASH-26) |
| API disabled by config | Daemon runs headless with no socket; `attach` reports "API disabled" (DASH-6) |

## 6. Configuration added in this milestone

| Setting | Flag | Env | Default |
|---------|------|-----|---------|
| API socket path | `--api-socket` | `WATCHER_API_SOCKET` | `${XDG_RUNTIME_DIR}/watcher.sock` |
| API enable/disable | `--api` | `WATCHER_API` | enabled |
| Database path | `--db` | `WATCHER_DB` | `./watcher.db` (OD-03-4) |
| Retention (age) | `--retention` | `WATCHER_RETENTION` | *OD-03-5* |
| Occurrences per incident kept | `--occurrence-cap` | `WATCHER_OCCURRENCE_CAP` | *OD-03-5* |
| UI refresh bound | `--refresh-interval` | `WATCHER_REFRESH_INTERVAL` | *OD-03-6* |
| List pane height fraction | `--list-fraction` | `WATCHER_LIST_FRACTION` | `0.45` |

## 7. Testing strategy

- **Persistence** — round-trip tests: insert incidents/explanations/occurrences,
  reopen the DB, assert equality; migration test from an empty DB; retention
  trimming test.
- **API** — `httptest` against the handler set: list shape, detail shape, 404
  for unknown fingerprint, health.
- **SSE** — assert an event is emitted per mutation and that a stalled reader
  does not block the daemon.
- **TUI model** — pure `Update`/`View` tests: drive `WindowSizeMsg` at many
  sizes (including below minimum) asserting no panic and expected pane sizes;
  drive key messages asserting selection and focus changes; assert sort order.
- **Reconnect** — fake API that drops the connection; assert the disconnected
  state appears and reconnection restores updates.
- **Dogfood** — run the daemon against a fixture log and drive `attach` with a
  scripted input sequence (bubbletea's test harness) to assert the rendered
  output contains the expected incident.

## 8. Open decisions

- **OD-03-1** — Whether to support loopback TCP (with a token) as an
  alternative to the Unix socket, or Unix-socket-only.
- **OD-03-2** — Exact pure-Go SQLite driver.
- **OD-03-3** — Whether counts/state resume from the database after a restart,
  or restart fresh for notification purposes (interacts with
  `../02-production-shape/design.md` §4.3).
- **OD-03-4** — Default database path (working directory vs. a state directory
  such as `/var/lib/watcher`).
- **OD-03-5** — Default retention policy: age cap, per-incident occurrence cap,
  or both.
- **OD-03-6** — Default UI refresh bound and the SSE watchdog interval.
- **OD-03-7** — Whether a later milestone should allow acknowledging/silencing
  incidents from the TUI (which would make `attach` no longer read-only and
  would add write routes to the API).
- **OD-03-8** — Whether severity should influence list ordering (this design
  keeps ordering by recency to avoid rows jumping).
- **OD-03-9** — Whether the TUI should show a severity/kind breakdown summary
  header, and if so what is most useful at a glance.
