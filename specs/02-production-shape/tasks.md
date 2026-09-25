# 02 — Production Shape: Tasks

Each task notes the requirements it satisfies. Read
[`design.md`](./design.md) before starting, and
`../01-core-engine/design.md` for the interfaces being extended.

## 1. Docker source

- [ ] **T-1.1** — Add a Docker client abstraction (`internal/source/docker`)
      over the socket; decide dependency per OD-02-6.
- [ ] **T-1.2** — Implement container listing by selector (name / label / all).
      *(PROD-SRC-1)*
- [ ] **T-1.3** — Implement the multiplexed frame reader (8-byte header,
      stdout/stderr, partial-line carry-over). *(PROD-SRC-2, PROD-SRC-3)*
- [ ] **T-1.4** — Handle TTY containers (raw stream, no framing).
      *(PROD-SRC-2)*
- [ ] **T-1.5** — Implement the container lifecycle supervisor over the events
      stream: attach on start/restart, detach on die/stop.
      *(PROD-SRC-4, PROD-SRC-5)*
- [ ] **T-1.6** — Tag lines with container identity for the `source` field.
      *(PROD-SRC-6)*
- [ ] **T-1.7** — Extend `guard.IsSelf` to container identity and refuse to
      attach to Watcher's own container. *(PROD-SRC-7)*
- [ ] **T-1.8** — Fail fast with a clear diagnostic when the socket is missing
      or unauthorized. *(PROD-SRC-8)*
- [ ] **T-1.9** — Implement the configurable lookback (`since`).
      *(PROD-SRC-9)*
- [ ] **T-1.10** — Reconnect with backoff after stream errors.
      *(PROD-NFR-3)*
- [ ] **T-1.11** — Tests: synthetic framed streams (split mid-line), lifecycle
      events, socket failure, TTY vs non-TTY.

## 2. Compose packaging

- [ ] **T-2.1** — Write the Compose file with `ollama`, `ollama-init`, and
      `watcher` services. *(PROD-PKG-1)*
- [ ] **T-2.2** — Add the `ollama` healthcheck and `depends_on` conditions.
      *(PROD-PKG-2)*
- [ ] **T-2.3** — Implement model auto-pull on first boot via `ollama-init`.
      *(PROD-PKG-3)*
- [ ] **T-2.4** — Add the named model volume. *(PROD-PKG-4)*
- [ ] **T-2.5** — Mount the Docker socket read-only; write the Dockerfile for
      `watcher`. *(PROD-PKG-5)*
- [ ] **T-2.6** — Wire all settable settings through environment variables.
      *(PROD-PKG-6)*
- [ ] **T-2.7** — Set `restart: unless-stopped` and verify operation with the
      model backend stopped. *(PROD-PKG-7, PROD-NFR-1)*
- [ ] **T-2.8** — Document the stack in the README (build, run, required env).

## 3. Incident state machine

- [ ] **T-3.1** — Add `NotificationKind` to the result model. *(PROD-WH-3)*
- [ ] **T-3.2** — Implement `New` state: emit exactly one `new` notification on
      first sight. *(PROD-STM-1)*
- [ ] **T-3.3** — Implement suppression + throttled `ongoing` emission with a
      running count. *(PROD-STM-2, PROD-STM-5)*
- [ ] **T-3.4** — Implement the resolve ticker and single `resolved`
      notification. *(PROD-STM-3)*
- [ ] **T-3.5** — Implement reopen-after-resolve as a fresh cycle.
      *(PROD-STM-4)*
- [ ] **T-3.6** — Make T and W configurable. *(PROD-STM-6)*
- [ ] **T-3.7** — Ensure restart never emits spurious `resolved`. *(PROD-STM-7)*
- [ ] **T-3.8** — Implement `explanation_pending` handling. *(PROD-STM-8)*
- [ ] **T-3.9** — Tests with an injectable clock covering all transitions.

## 4. Webhook sink

- [ ] **T-4.1** — Implement the fan-out multiplexer; one sink's failure must
      not suppress the others. *(PROD-WH-8)*
- [ ] **T-4.2** — Implement HTTP delivery with configurable URL.
      *(PROD-WH-1)*
- [ ] **T-4.3** — Implement the Slack-shaped payload builder.
      *(PROD-WH-2)*
- [ ] **T-4.4** — Implement the generic JSON payload builder. *(PROD-WH-2)*
- [ ] **T-4.5** — Enforce state-machine gating so only new/ongoing/resolved are
      delivered. *(PROD-WH-3)*
- [ ] **T-4.6** — Implement retry with exponential backoff + jitter.
      *(PROD-WH-4)*
- [ ] **T-4.7** — Implement the JSONL fallback file on final failure.
      *(PROD-WH-5)*
- [ ] **T-4.8** — Implement the bounded delivery queue and drop-oldest policy
      with a logged dropped counter. *(PROD-WH-6, PROD-NFR-2)*
- [ ] **T-4.9** — Enforce payload field completeness with explicit nulls for
      unavailable explanation fields. *(PROD-WH-7)*
- [ ] **T-4.10** — Cap log-derived payload text length. *(PROD-WH-9)*
- [ ] **T-4.11** — Tests: payload golden files for both formats, retry timing
      with a fake clock, fallback contents, full-queue drop behavior.

## 5. Heartbeat

- [ ] **T-5.1** — Implement the heartbeat ticker, disabled without a URL.
      *(PROD-HB-1, PROD-HB-5)*
- [ ] **T-5.2** — Build the liveness-counter payload.
      *(PROD-HB-2)*
- [ ] **T-5.3** — Ensure the heartbeat schedule is independent of incident
      traffic. *(PROD-HB-3)*
- [ ] **T-5.4** — Log-and-continue on failure; no unbounded retry.
      *(PROD-HB-4)*
- [ ] **T-5.5** — Tests with a fake clock and `httptest` receiver.

## 6. Configuration and observability

- [ ] **T-6.1** — Extend `internal/config` with the table in `design.md` §7.
      *(PROD-NFR-4)*
- [ ] **T-6.2** — Emit startup diagnostics (sources, sinks, model) to stderr.
      *(PROD-NFR-5)*
- [ ] **T-6.3** — Bound memory for stream buffers and queues.
      *(PROD-NFR-2)*

## 7. Integration

- [ ] **T-7.1** — Bring the stack up locally with a crash-looping test
      container and a receiver; assert one `new`, throttled `ongoing`, and a
      final `resolved`.
- [ ] **T-7.2** — Verify detection and counting continue with Ollama stopped.
      *(PROD-NFR-1)*
- [ ] **T-7.3** — Verify the socket-error path fails fast and clearly.
      *(PROD-SRC-8)*
- [ ] **T-7.4** — Verify a restart produces no spurious `resolved` burst.
      *(PROD-STM-7)*

## 8. Exit criteria

- [ ] Every `PROD-*` requirement has a passing test.
- [ ] `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` passes.
- [ ] The stack runs from a clean machine with `docker compose up` and no
      manual model pull.
- [ ] A crash-looping container yields exactly one `new` notification, periodic
      `ongoing` updates, and one `resolved`.
- [ ] Resolve the open decisions in `design.md` §10 and update the spec with
      the chosen values.
