# 01b — Runtime: Tasks

Each task notes the requirements it satisfies. Read
[`design.md`](./design.md) before starting; it holds the rationale, including
the rejected content-inference approach (§10) and the compatibility notes (§11).

Suggested order: CLI → multi-source → Docker source → webhook → verification.

> **Status:** implemented in the `01b-runtime` development run. The two live
> verification tasks (T-6.1, T-6.2) require an evaluation-harness run against a
> real Docker daemon and are not exercised by the unit suite; see
> `docs/dev-runs/01b-runtime.md`. The resolved open decisions are recorded in
> `design.md` §12.

## 1. CLI

- [x] **T-1.1** — Introduce the verb registry and dispatch; register `run`.
      *(RT-CLI-1, RT-CLI-6)*
- [x] **T-1.2** — Make the bare invocation an alias for `run`, preserving all
      existing flags. *(RT-CLI-2)*
- [x] **T-1.3** — Unknown verb: non-zero exit with the valid-verb list.
      *(RT-CLI-3)*
- [x] **T-1.4** — `--help` lists verbs and `run`'s flags. *(RT-CLI-4)*
- [x] **T-1.5** — Preserve the exit-code contract. *(RT-CLI-5)*
- [x] **T-1.6** — Tests: dispatch table, alias equivalence, unknown verb, exit
      codes.

## 2. Labeled multi-source input

- [x] **T-2.1** — Add a label to the standard sources (stdin, file); default a
      lone file to its path and stdin to `stdin`. *(RT-SRC-1, RT-SRC-9)*
- [x] **T-2.2** — Accept several sources and mixes of files and stdin; reject
      duplicate labels at startup. *(RT-SRC-2, RT-SRC-4)*
- [x] **T-2.3** — Extend the engine to run one feeder + ring + detector per
      source, feeding a shared event channel. *(RT-SRC-3, RT-SRC-8)*
- [x] **T-2.4** — Key the incident tracker on `(label, fingerprint)`; leave the
      fingerprint hash unchanged. *(RT-SRC-3, RT-SRC-5)*
- [x] **T-2.5** — One source reaching EOF must not stop the others; exit 0 only
      when all end. *(RT-SRC-6)*
- [x] **T-2.6** — Ensure attribution comes only from the label. *(RT-SRC-7)*
- [x] **T-2.7** — Tests: two sources do not cross-contaminate blocks; same crash
      on two labels yields two incidents; EOF and cancellation behaviour.

## 3. Docker container source

- [x] **T-3.1** — Docker client over the socket (decide dependency per
      OD-01B-8). *(RT-DOCK-1)*
- [x] **T-3.2** — Resolve Watcher's own container id and Compose project label.
      *(RT-DOCK-2)*
- [x] **T-3.3** — Default scope: attach to same-project containers when in one;
      never all containers on the host. *(RT-DOCK-2, RT-DOCK-3)*
- [x] **T-3.4** — Explicit selector (name / label / project) and a disable
      switch. *(RT-DOCK-3)*
- [x] **T-3.5** — Multiplexed frame reader with partial-line carry-over; TTY raw
      stream. *(RT-DOCK-4)*
- [x] **T-3.6** — Lifecycle supervisor: attach on start/restart, detach on
      die/stop. *(RT-DOCK-5)*
- [x] **T-3.7** — Label lines with container identity. *(RT-DOCK-6)*
- [x] **T-3.8** — Extend `guard.IsSelf` to container identity. *(RT-DOCK-7)*
- [x] **T-3.9** — Fail fast when a requested socket is missing/unauthorized;
      never fail a non-Docker run. *(RT-DOCK-8)*
- [x] **T-3.10** — Reconnect with backoff; configurable lookback.
      *(RT-DOCK-9, RT-DOCK-10)*
- [x] **T-3.11** — Tests: synthetic framed streams, lifecycle events, default
      scope, socket failure, TTY vs non-TTY.

## 4. Webhook sink

- [x] **T-4.1** — Sink plumbing: disabled without a URL; fan-out to configured
      sinks independently. *(RT-WH-1, RT-WH-9)*
- [x] **T-4.2** — Generic JSON payload with the full field set and explicit
      nulls for unavailable explanations. *(RT-WH-2, RT-WH-3)*
- [x] **T-4.3** — Slack payload builder. *(RT-WH-2)*
- [x] **T-4.4** — Discord payload builder. *(RT-WH-2)*
- [x] **T-4.5** — Per-provider length caps with visible truncation.
      *(RT-WH-4)*
- [x] **T-4.6** — Retry with exponential backoff + jitter, honouring
      `Retry-After`. *(RT-WH-5)*
- [x] **T-4.7** — JSONL fallback file on exhaustion, including the failure
      reason. *(RT-WH-6)*
- [x] **T-4.8** — Bounded queue with drop-oldest and a logged dropped counter.
      *(RT-WH-7)*
- [x] **T-4.9** — Minimal throttle: one notification per `(label, fingerprint)`
      per window, counting every occurrence regardless. *(RT-WH-8)*
- [x] **T-4.10** — Tests: per-provider payload goldens, retry timing with a fake
      clock, fallback contents, full-queue drop, throttle suppression.

## 5. Configuration and diagnostics

- [x] **T-5.1** — Add the settings from `design.md` §7 to `internal/config`.
      *(RT-CFG-1)*
- [x] **T-5.2** — Keep `--file` working as a single-source shorthand.
      *(RT-CFG-3)*
- [x] **T-5.3** — Keep precedence `flag > env > default`. *(RT-CFG-2)*
- [x] **T-5.4** — Startup diagnostics to stderr: sources attached, sinks
      configured. *(RT-CFG-4)*

## 6. Live verification

- [ ] **T-6.1** — Verify against the evaluation harness: Watcher attaches to the
      victim's Compose project and survives the harness recreating containers.
      *(RT-VER-1)* — requires a live Docker/Compose run.
- [ ] **T-6.2** — Verify the output is consumable by the harness scorer and
      produces a pass/fail. *(RT-VER-2)* — requires a live Docker/Compose run.
- [x] **T-6.3** — Document the verification procedure. *(RT-VER-3)* — in the
      README's evaluation-harness section and the run report.

## 7. Exit criteria

- [ ] Every `RT-*` requirement has a passing test. *(all except RT-VER-1/2,
      which are deliberate live checks)*
- [x] `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` passes.
- [x] `watcher run` and the bare invocation both start the daemon; the 01
      invocations still work.
- [x] One instance watches several labeled sources; a crash on one label is
      reported with that label.
- [ ] In a Compose deployment, zero-argument `watcher run` attaches to the
      project's siblings and survives their recreation. — requires a live run.
- [x] A crash loop produces one notification per throttle window, not one per
      occurrence. *(covered at the throttle and sink level; the live end-to-end
      run is the remaining check)*
- [x] Resolve the open decisions in `design.md` §12 and update the spec.
