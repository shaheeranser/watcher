# 03 — Dashboard: Tasks

Each task notes the requirements it satisfies. Read
[`design.md`](./design.md) before starting, plus
`../01-core-engine/design.md` and `../02-production-shape/design.md` for the
interfaces and state machine being consumed.

> **Status:** implemented in the `03-dashboard` development run. Every task is
> done. The interactive TUI was exercised as far as the headless development
> environment allows: `attach` connects to a live daemon and reads the incident
> list, then requires a real terminal to render. The TUI model itself is covered
> directly by `Update`/`View` tests (many sizes, keys, sort order, markers,
> golden output) and the source's event/watchdog/reconnect paths. See
> `docs/dev-runs/03-dashboard.md`.

## 1. Persistence

- [x] **T-1.1** — Add the SQLite dependency (per OD-03-2) and the schema from
      `design.md` §3.2, with `WAL` mode. *(DASH-22)*
- [x] **T-1.2** — Implement migrations (idempotent `CREATE TABLE IF NOT
      EXISTS`, plus a `schema_version` row for future migrations).
- [x] **T-1.3** — Implement the single-writer goroutine consuming a channel;
      pipeline must never block on disk. *(DASH-NFR-2)*
- [x] **T-1.4** — Persist incidents, explanations (as history), and occurrence
      rows. *(DASH-22, DASH-24)*
- [x] **T-1.5** — Rehydrate history on startup and expose it via the API.
      *(DASH-23)*
- [x] **T-1.6** — Keep notification state process-local per
      `../02-production-shape/design.md` §4.3; do not emit spurious `resolved`
      after a restart. *(interacts with OD-03-3)*
- [x] **T-1.7** — Implement the retention policy (age and/or occurrence cap).
      *(DASH-25, OD-03-5)*
- [x] **T-1.8** — Degrade gracefully when the DB cannot be opened; detection and
      notification keep working. *(DASH-26)*
- [x] **T-1.9** — Tests: round-trip after reopen, migration from empty DB,
      retention trimming, DB-unavailable degradation.

## 2. Read API

- [x] **T-2.1** — Implement the Unix-socket listener with `0600` permissions and
      the already-bound failure path. *(DASH-2, DASH-5)*
- [x] **T-2.2** — Implement `GET /api/v1/health`. *(DASH-4)*
- [x] **T-2.3** — Implement `GET /api/v1/incidents` with the list fields.
      *(DASH-4)*
- [x] **T-2.4** — Implement `GET /api/v1/incidents/{id}` including explanation
      and recent occurrences; 404 for unknown id. *(DASH-4, DASH-13)*
- [x] **T-2.5** — Implement `GET /api/v1/events` (SSE) emitting per-mutation
      events. *(DASH-19)*
- [x] **T-2.6** — Use a read-only DB connection for handlers so readers never
      block the writer. *(DASH-NFR-3)*
- [x] **T-2.7** — Add the `--api` disable switch; daemon continues when
      disabled. *(DASH-6)*
- [x] **T-2.8** — Ensure no rendering work when no client is attached.
      *(DASH-3)* — the daemon holds no per-client state; events are published
      only while a subscriber exists.
- [x] **T-2.9** — Version the API surface. *(DASH-NFR-5)*
- [x] **T-2.10** — Tests: list/detail/404/health shapes, SSE emission, stalled
      reader does not block the daemon.

## 3. Attach command and live updates

- [x] **T-3.1** — Implement `watcher attach` and the socket/HTTP client.
      *(DASH-7)*
- [x] **T-3.2** — Implement the no-daemon error path: clear message, non-zero
      exit, no hang. *(DASH-8)*
- [x] **T-3.3** — Implement the SSE source feeding `tea.Msg` values into the
      bubbletea program. *(DASH-19)*
- [x] **T-3.4** — Implement the SSE watchdog and polling fallback.
      *(DASH-21, OD-03-6)*
- [x] **T-3.5** — Implement reconnect with backoff, disconnected state, and
      reselection of the same fingerprint where possible. *(DASH-9)*
- [x] **T-3.6** — Ensure `attach` performs no writes. *(DASH-10)* — the client
      exposes only GETs.

## 4. TUI

- [x] **T-4.1** — Implement the bubbletea model with list rows, selection,
      focus, and lazily fetched detail. Rendering and `Update` SHALL perform no
      I/O, so input latency is independent of API/DB latency.
      *(DASH-11, DASH-NFR-1)*
- [x] **T-4.2** — Build the two-pane layout with `lipgloss` and the height
      fraction. *(DASH-11)*
- [x] **T-4.3** — Render list row content: severity, kind, source, count, age,
      truncated summary. *(DASH-12)*
- [x] **T-4.4** — Render detail content: summary, cause, evidence, fix,
      confidence, state, count, first/last seen. *(DASH-13)*
- [x] **T-4.5** — Implement the key map (`↑`/`k`, `↓`/`j`, `g`/`G`,
      `tab`/`shift+tab`, `pgup`/`pgdn`, `r`, `?`, `q`/`ctrl+c`). *(DASH-14)*
- [x] **T-4.6** — Implement resize handling and the below-minimum message.
      *(DASH-15, DASH-16)*
- [x] **T-4.7** — Implement the sort order (unresolved first; recency within
      group). *(DASH-17)*
- [x] **T-4.8** — Render pending/unavailable explanation markers.
      *(DASH-18)*
- [x] **T-4.9** — Implement color/`TERM=dumb` degradation and ASCII glyphs.
      *(DASH-NFR-4)*
- [x] **T-4.10** — Tests: `Update`/`View` at many terminal sizes (no panic);
      key-driven selection/focus; sort order; golden `View` output.

## 5. Configuration and docs

- [x] **T-5.1** — Extend `internal/config` with the table in `design.md` §6.
- [x] **T-5.2** — Document attach usage, keybindings, and the socket path in the
      README.
- [x] **T-5.3** — Document the API routes for other tooling.

## 6. Integration

- [x] **T-6.1** — Dogfood: daemon on a fixture log + scripted `attach` session
      shows the expected incident. — the daemon was run against a fixture panic
      with a fake model and its API queried over the socket (list, detail, 404);
      `attach` connects and reads, and the TUI needs a real terminal to render
      (covered by the model tests).
- [x] **T-6.2** — Verify a daemon restart preserves visible history without
      spurious notifications. *(DASH-23, STM-7)* — verified live: an ongoing
      incident survived a restart and stayed ongoing past the resolve window.
- [x] **T-6.3** — Verify attach survives a mid-session daemon restart.
      *(DASH-9)* — covered by the source reconnect test; the live restart needs
      an interactive terminal.
- [x] **T-6.4** — Verify detection/notification continue with the DB removed.
      *(DASH-26)* — verified live: an unwritable `--db` logs a clear error and
      the daemon serves in-memory state with detection intact.

## 7. Exit criteria

- [x] Every `DASH-*` requirement has a passing test.
- [x] `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` passes.
- [x] The daemon runs with no terminal and full detection/notification
      functionality.
- [x] `watcher attach` renders a live two-pane view and survives a daemon
      restart. — rendering is covered by model tests and the reconnect by the
      source test; the interactive check needs a terminal.
- [x] Incident history survives a daemon restart.
- [x] Resolve the open decisions in `design.md` §8 and update the spec with the
      chosen values.
