# 01b — Runtime: Requirements

## 1. Purpose

Make Watcher actually runnable as a daemon. Milestone 01 delivered the pipeline
but left it a single-source, single-invocation command: there is no `run` verb,
one Watcher instance can watch only one named source, and there is no outbound
channel. This milestone adds the runtime interfaces the rest of the product
builds on:

- a verb-dispatched CLI whose `run` command is the daemon entry point;
- **labeled multi-source** input — several files, or a mix of files and stdin,
  each attributed to an operator-assigned label;
- a **Docker container-log source** whose zero-configuration default is to
  attach to Watcher's own Compose project's siblings;
- a **webhook sink** (Slack, Discord, generic JSON) with bounded delivery and a
  minimal notification throttle;
- enough of the above to **verify the pipeline live** against the external
  evaluation harness.

This is the bridge between the core engine (01) and production shape (02): it
establishes the runtime seams, and 02, 03, and 05 extend them rather than
redefining them.

## 2. Scope

**In scope**

- CLI verb dispatch; `run` as the daemon; the bare invocation kept as an alias.
- Multiple labeled sources watched by one instance, including Docker.
- A Docker container-log `Source` with Compose-project default scope.
- A webhook `Sink` with per-provider payloads, retry/backoff, a fallback file,
  and a minimal per-incident notification throttle.
- A documented live path for scoring against the evaluation harness.

**Out of scope (deferred)**

- **02** — Compose packaging, the `new`/`ongoing`/`resolved` incident state
  machine, the heartbeat. 02 *replaces* this milestone's minimal throttle.
- **03** — persistence, the read API, `watcher attach`.
- **04** — `watcher eval`.
- **05** — the installer, `watcher onboard`, the config file, the systemd unit.

Requirement IDs use the form `RT-<AREA>-<n>`.

## 3. Functional requirements

### 3.1 CLI (`RT-CLI`)

- **RT-CLI-1** — THE SYSTEM SHALL treat its first non-flag argument as a verb,
  and `run` SHALL start the long-running daemon.
- **RT-CLI-2** — WHEN invoked with no verb, THE SYSTEM SHALL behave exactly as
  `run`, preserving the existing flag behaviour so current invocations keep
  working.
- **RT-CLI-3** — WHEN given an unknown verb, THE SYSTEM SHALL exit non-zero with
  a message listing the valid verbs.
- **RT-CLI-4** — `--help` SHALL list the verbs and, for `run`, its flags.
- **RT-CLI-5** — THE SYSTEM SHALL preserve the exit-code contract: 0 on clean
  shutdown, non-zero on fatal startup errors.
- **RT-CLI-6** — Verb dispatch SHALL be the single registration point for
  future verbs (`onboard`, `eval`, `attach`); this milestone registers only
  `run`.

### 3.2 Labeled multi-source input (`RT-SRC`)

- **RT-SRC-1** — `run` SHALL accept one or more sources, each with an
  operator-assigned label.
- **RT-SRC-2** — THE SYSTEM SHALL support several file sources and a mix of
  file sources and stdin in one instance.
- **RT-SRC-3** — Every emitted `Line` SHALL carry its source's label, and that
  label SHALL be threaded through detection, fingerprinting, and incident
  tracking to the emitted result, so a report attributes the origin.
- **RT-SRC-4** — Duplicate labels SHALL be rejected at startup with a clear
  error.
- **RT-SRC-5** — WHEN the same crash text arrives on two different labels, THE
  SYSTEM SHALL track two distinct incidents, each attributed to its label.
- **RT-SRC-6** — WHEN one source ends (EOF), THE SYSTEM SHALL keep running
  while any other source is still active, and SHALL exit 0 only when all
  sources have ended; cancellation still stops it immediately.
- **RT-SRC-7** — THE SYSTEM SHALL determine a line's origin solely from its
  source's label; it SHALL NOT infer origin from log content.
- **RT-SRC-8** — Each source SHALL have independent, bounded buffering, and a
  stalled source SHALL NOT stall the others beyond bounded backpressure.
- **RT-SRC-9** — WHEN exactly one file source is configured without an explicit
  label, THE SYSTEM SHALL default its label to the file path; stdin SHALL
  default to `stdin`.

### 3.3 Docker container source (`RT-DOCK`)

- **RT-DOCK-1** — THE SYSTEM SHALL stream container logs through the Docker
  Engine API over the Docker socket, without requiring the `docker` CLI binary.
- **RT-DOCK-2** — WHEN `run` is started with no source configuration inside a
  container that carries a Compose project label, THE SYSTEM SHALL default to
  attaching to the containers in that **same Compose project**, and SHALL NOT
  attach to unrelated containers on the host.
- **RT-DOCK-3** — THE SYSTEM SHALL accept an explicit selector (by name, by
  label, or by Compose project) that overrides the default, and SHALL support
  disabling the Docker source entirely.
- **RT-DOCK-4** — WHEN a container's stream is multiplexed (non-TTY), THE SYSTEM
  SHALL demultiplex the frame header, preserve stdout/stderr distinction, and
  carry partial lines across frames; TTY containers SHALL be read as a raw
  stream.
- **RT-DOCK-5** — WHEN a selected container starts or restarts, THE SYSTEM SHALL
  begin streaming it without operator action; WHEN it stops, THE SYSTEM SHALL
  end that stream and keep serving the others.
- **RT-DOCK-6** — THE SYSTEM SHALL label each line with the container's
  identity (Compose service name, or container name), usable as the incident
  label.
- **RT-DOCK-7** — THE SYSTEM SHALL NOT attach to Watcher's own container,
  extending the milestone-01 guard to container identity.
- **RT-DOCK-8** — WHEN a Docker source is requested and the socket is missing,
  inaccessible, or unauthorized, THE SYSTEM SHALL fail at startup with a clear
  stderr diagnostic and a non-zero exit.
- **RT-DOCK-9** — THE SYSTEM SHALL reconnect after a transient stream error
  with backoff and without operator action.
- **RT-DOCK-10** — THE SYSTEM SHALL support a configurable lookback so it may
  start from a bounded amount of existing container output.

### 3.4 Webhook sink (`RT-WH`)

- **RT-WH-1** — WHEN a webhook URL is configured, THE SYSTEM SHALL deliver
  notifications to it; the sink SHALL be disabled when no URL is set.
- **RT-WH-2** — THE SYSTEM SHALL support three payload providers, selectable by
  configuration: `slack`, `discord`, and `generic` JSON.
- **RT-WH-3** — Every payload SHALL contain at least: notification/event kind,
  detector kind, source label, fingerprint, severity, count, first-seen,
  last-seen, summary, likely cause, suggested fix, confidence, and model; an
  unavailable explanation SHALL be represented explicitly, not omitted.
- **RT-WH-4** — THE SYSTEM SHALL cap log-derived text per provider's limits
  (e.g. Discord's `content` length, Slack's block limits), marking truncation.
- **RT-WH-5** — WHEN a delivery attempt fails (non-2xx, timeout, connection
  error), THE SYSTEM SHALL retry with exponential backoff and jitter up to a
  configured maximum, honouring `Retry-After` when present.
- **RT-WH-6** — WHEN delivery still fails after the final attempt, THE SYSTEM
  SHALL append the notification, with the failure reason, as one JSON line to a
  local fallback file, and continue.
- **RT-WH-7** — Webhook delivery SHALL NOT block detection: it SHALL use a
  bounded queue with a documented drop policy and a logged dropped counter.
- **RT-WH-8** — UNTIL the milestone-02 state machine exists, THE SYSTEM SHALL
  emit at most one notification per `(label, fingerprint)` per configurable
  throttle window, while still counting every occurrence.
- **RT-WH-9** — WHEN more than one sink is configured, THE SYSTEM SHALL fan out
  independently so one failing sink does not suppress the others.

### 3.5 Configuration (`RT-CFG`)

- **RT-CFG-1** — THE SYSTEM SHALL add settings for: repeatable sources
  (`--source label=path`), the Docker selector and socket host, the Docker
  lookback, the webhook URL/format/retries/backoff/fallback, and the
  notification throttle window.
- **RT-CFG-2** — Precedence SHALL remain `flag > environment > default`; a
  config-file rung is added by milestone 05, not here.
- **RT-CFG-3** — The existing `--file` flag SHALL keep working as a shorthand
  for a single file source, so 01's documented invocation is unchanged.
- **RT-CFG-4** — THE SYSTEM SHALL emit startup diagnostics to stderr naming the
  sources attached and the sinks configured.

### 3.6 Live verification (`RT-VER`)

- **RT-VER-1** — THE SYSTEM SHALL be able to attach to the evaluation harness's
  Compose project and continue across the harness recreating its containers, so
  a scenario run produces an incident without operator intervention.
- **RT-VER-2** — The emitted JSON Lines SHALL remain consumable by the
  harness's scorer (a record carrying a usable `likely_cause`), so a full
  scenario pass/fail can be produced.
- **RT-VER-3** — A documented procedure SHALL describe running the harness
  scenarios against a live Watcher and reading the score.

## 4. Non-functional requirements

- **RT-NFR-1** — Shutdown on `SIGINT`/`SIGTERM` SHALL flush in-flight work and
  stop every source's goroutines without leaking goroutines.
- **RT-NFR-2** — Memory SHALL stay bounded: per-source buffers and the sink
  queue are bounded, and a wedged webhook SHALL NOT grow the process.
- **RT-NFR-3** — Detection SHALL NOT block on the backend or on the webhook;
  source-to-detection latency SHALL be independent of both.
- **RT-NFR-4** — The binary SHALL keep building as a single static binary with
  `CGO_ENABLED=0`.
- **RT-NFR-5** — Unit tests SHALL run offline: a fake Docker API over a socket
  listener and `httptest` for the webhook, never a live Docker daemon or Ollama.

## 5. Traceability

Mechanism rationale — including the rejected content-inference approach — is in
[`design.md`](./design.md); the task breakdown is in [`tasks.md`](./tasks.md).
