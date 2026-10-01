# 02 — Production Shape: Tasks

Each task notes the requirements it satisfies. Read
[`design.md`](./design.md) before starting, and
`../01b-runtime/design.md` for the runtime pieces (the `run` verb, the labeled
multi-source engine, the Docker source, and the webhook sink) this milestone
builds on.

## 1. Compose packaging

- [ ] **T-1.1** — Write the Compose file with `ollama`, `ollama-init`, and
      `watcher` services. *(PROD-PKG-1)*
- [ ] **T-1.2** — Add the `ollama` healthcheck and `depends_on` conditions.
      *(PROD-PKG-2)*
- [ ] **T-1.3** — Implement model auto-pull on first boot via `ollama-init`.
      *(PROD-PKG-3)*
- [ ] **T-1.4** — Add the named model volume. *(PROD-PKG-4)*
- [ ] **T-1.5** — Mount the Docker socket read-only; write the `watcher`
      Dockerfile with entrypoint `watcher run`. *(PROD-PKG-5)*
- [ ] **T-1.6** — Wire all settable settings through environment variables.
      *(PROD-PKG-6)*
- [ ] **T-1.7** — Set `restart: unless-stopped` and verify operation with the
      model backend stopped. *(PROD-PKG-7, PROD-NFR-1)*
- [ ] **T-1.8** — Document the stack in the README (build, run, required env).

## 2. Incident state machine

- [ ] **T-2.1** — Add `NotificationKind` (`new`/`ongoing`/`resolved`) to the
      result model. *(PROD-STM-1)*
- [ ] **T-2.2** — Implement `New` state: emit exactly one `new` notification on
      first sight. *(PROD-STM-1)*
- [ ] **T-2.3** — Implement suppression + throttled `ongoing` emission with a
      running count. *(PROD-STM-2, PROD-STM-5)*
- [ ] **T-2.4** — Implement the resolve ticker and single `resolved`
      notification. *(PROD-STM-3)*
- [ ] **T-2.5** — Implement reopen-after-resolve as a fresh cycle.
      *(PROD-STM-4)*
- [ ] **T-2.6** — Make `T` and `W` configurable. *(PROD-STM-6)*
- [ ] **T-2.7** — Ensure restart never emits spurious `resolved`. *(PROD-STM-7)*
- [ ] **T-2.8** — Implement `explanation_pending` handling. *(PROD-STM-8)*
- [ ] **T-2.9** — Retire 01b's standalone throttle gating so this state machine
      is the only delivery policy. *(PROD-STM-9)*
- [ ] **T-2.10** — Tests with an injectable clock covering all transitions.

## 3. Heartbeat

- [ ] **T-3.1** — Implement the heartbeat ticker, disabled without a URL.
      *(PROD-HB-1, PROD-HB-5)*
- [ ] **T-3.2** — Build the liveness-counter payload. *(PROD-HB-2)*
- [ ] **T-3.3** — Ensure the heartbeat schedule is independent of incident
      traffic. *(PROD-HB-3)*
- [ ] **T-3.4** — Log-and-continue on failure; no unbounded retry.
      *(PROD-HB-4)*
- [ ] **T-3.5** — Tests with a fake clock and `httptest` receiver.

## 4. Configuration and observability

- [ ] **T-4.1** — Extend `internal/config` with the table in `design.md` §5.
      *(PROD-NFR-3)*
- [ ] **T-4.2** — Emit startup diagnostics (sinks, model, state-machine
      windows) to stderr. *(PROD-NFR-4)*
- [ ] **T-4.3** — Bound memory for the notification queue and heartbeat path.
      *(PROD-NFR-2)*

## 5. Integration

- [ ] **T-5.1** — Bring the stack up locally with a crash-looping test
      container and a receiver; assert one `new`, throttled `ongoing`, and a
      final `resolved`.
- [ ] **T-5.2** — Verify detection and counting continue with Ollama stopped.
      *(PROD-NFR-1)*
- [ ] **T-5.3** — Verify a restart produces no spurious `resolved` burst.
      *(PROD-STM-7)*
- [ ] **T-5.4** — Verify the stack runs from a clean machine with
      `docker compose up` and no manual model pull. *(PROD-PKG-3)*

## 6. Exit criteria

- [ ] Every `PROD-*` requirement has a passing test.
- [ ] `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` passes.
- [ ] The stack runs from a clean machine with `docker compose up` and no
      manual model pull.
- [ ] A crash-looping container yields exactly one `new` notification, periodic
      `ongoing` updates, and one `resolved`.
- [ ] Resolve the open decisions in `design.md` §8 and update the spec with the
      chosen values.
