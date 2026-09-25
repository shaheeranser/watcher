# 03 — Dashboard: Tasks

Each task notes the requirements it satisfies. Read
[`design.md`](./design.md) before starting, plus
`../01-core-engine/design.md` and `../02-production-shape/design.md` for the
interfaces and state machine being consumed.

## 1. Persistence

- [ ] **T-1.1** — Add the SQLite dependency (per OD-03-2) and the schema from
      `design.md` §3.2, with `WAL` mode. *(DASH-22)*
- [ ] **T-1.2** — Implement migrations (idempotent `CREATE TABLE IF NOT
      EXISTS`, plus a `schema_version` row for future migrations).
- [ ] **T-1.3** — Implement the single-writer goroutine consuming a channel;
      pipeline must never block on disk. *(DASH-NFR-2)*
- [ ] **T-1.4** — Persist incidents, explanations (as history), and occurrence
      rows. *(DASH-22, DASH-24)*
- [ ] **T-1.5** — Rehydrate history on startup and expose it via the API.
      *(DASH-23)*
- [ ] **T-1.6** — Keep notification state process-local per
      `../02-production-shape/design.md` §4.3; do not emit spurious `resolved`
      after a restart. *(interacts with OD-03-3)*
- [ ] **T-1.7** — Implement the retention policy (age and/or occurrence cap).
      *(DASH-25, OD-03-5)*
- [ ] **T-1.8** — Degrade gracefully when the DB cannot be opened; detection and
      notification keep working. *(DASH-26)*
- [ ] **T-1.9** — Tests: round-trip after reopen, migration from empty DB,
      retention trimming, DB-unavailable degradation.

## 2. Read API

- [ ] **T-2.1** — Implement the Unix-socket listener with `0600` permissions and
      the already-bound failure path. *(DASH-2, DASH-5)*
- [ ] **T-2.2** — Implement `GET /api/v1/health`. *(DASH-4)*
- [ ] **T-2.3** — Implement `GET /api/v1/incidents` with the list fields.
      *(DASH-4)*
- [ ] **T-2.4** — Implement `GET /api/v1/incidents/{id}` including explanation
      and recent occurrences; 404 for unknown id. *(DASH-4, DASH-13)*
- [ ] **T-2.5** — Implement `GET /api/v1/events` (SSE) emitting per-mutation
      events. *(DASH-19)*
- [ ] **T-2.6** — Use a read-only DB connection for handlers so readers never
      block the writer. *(DASH-NFR-3)*
- [ ] **T-2.7** — Add the `--api` disable switch; daemon continues when
      disabled. *(DASH-6)*
- [ ] **T-2.8** — Ensure no rendering work when no client is attached.
      *(DASH-3)*
- [ ] **T-2.9** — Version the API surface. *(DASH-NFR-5)*
- [ ] **T-2.10** — Tests: list/detail/404/health shapes, SSE emission, stalled
      reader does not block the daemon.

## 3. Attach command and live updates

- [ ] **T-3.1** — Implement `watcher attach` and the socket/HTTP client.
      *(DASH-7)*
- [ ] **T-3.2** — Implement the no-daemon error path: clear message, non-zero
      exit, no hang. *(DASH-8)*
- [ ] **T-3.3** — Implement the SSE source feeding `tea.Msg` values into the
      bubbletea program. *(DASH-19)*
- [ ] **T-3.4** — Implement the SSE watchdog and polling fallback.
      *(DASH-21, OD-03-6)*
- [ ] **T-3.5** — Implement reconnect with backoff, disconnected state, and
      reselection of the same fingerprint where possible. *(DASH-9)*
- [ ] **T-3.6** — Ensure `attach` performs no writes. *(DASH-10)*

## 4. TUI

- [ ] **T-4.1** — Implement the bubbletea model with list rows, selection,
      focus, and lazily fetched detail. Rendering and `Update` SHALL perform no
      I/O, so input latency is independent of API/DB latency.
      *(DASH-11, DASH-NFR-1)*
- [ ] **T-4.2** — Build the two-pane layout with `lipgloss` and the height
      fraction. *(DASH-11)*
- [ ] **T-4.3** — Render list row content: severity, kind, source, count, age,
      truncated summary. *(DASH-12)*
- [ ] **T-4.4** — Render detail content: summary, cause, evidence, fix,
      confidence, state, count, first/last seen. *(DASH-13)*
- [ ] **T-4.5** — Implement the key map (`↑`/`k`, `↓`/`j`, `g`/`G`,
      `tab`/`shift+tab`, `pgup`/`pgdn`, `r`, `?`, `q`/`ctrl+c`). *(DASH-14)*
- [ ] **T-4.6** — Implement resize handling and the below-minimum message.
      *(DASH-15, DASH-16)*
- [ ] **T-4.7** — Implement the sort order (unresolved first; recency within
      group). *(DASH-17)*
- [ ] **T-4.8** — Render pending/unavailable explanation markers.
      *(DASH-18)*
- [ ] **T-4.9** — Implement color/`TERM=dumb` degradation and ASCII glyphs.
      *(DASH-NFR-4)*
- [ ] **T-4.10** — Tests: `Update`/`View` at many terminal sizes (no panic);
      key-driven selection/focus; sort order; golden `View` output.

## 5. Configuration and docs

- [ ] **T-5.1** — Extend `internal/config` with the table in `design.md` §6.
- [ ] **T-5.2** — Document attach usage, keybindings, and the socket path in the
      README.
- [ ] **T-5.3** — Document the API routes for other tooling.

## 6. Integration

- [ ] **T-6.1** — Dogfood: daemon on a fixture log + scripted `attach` session
      shows the expected incident.
- [ ] **T-6.2** — Verify a daemon restart preserves visible history without
      spurious notifications. *(DASH-23, STM-7)*
- [ ] **T-6.3** — Verify attach survives a mid-session daemon restart.
      *(DASH-9)*
- [ ] **T-6.4** — Verify detection/notification continue with the DB removed.
      *(DASH-26)*

## 7. Exit criteria

- [ ] Every `DASH-*` requirement has a passing test.
- [ ] `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` passes.
- [ ] The daemon runs with no terminal and full detection/notification
      functionality.
- [ ] `watcher attach` renders a live two-pane view and survives a daemon
      restart.
- [ ] Incident history survives a daemon restart.
- [ ] Resolve the open decisions in `design.md` §8 and update the spec with the
      chosen values.
