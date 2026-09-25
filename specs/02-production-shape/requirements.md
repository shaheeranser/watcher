# 02 — Production Shape: Requirements

## 1. Purpose

Turn the local-only core engine into something that runs unattended in a real
deployment: read logs from Docker containers, ship as a Compose stack with
Ollama, notify without spamming when something crash-loops, and make
Watcher's own failure visible.

## 2. Scope

**In scope**

- A Docker container-log `Source` built on the Docker Engine API.
- Docker Compose packaging of `watcher` + `ollama` with model auto-pull.
- The incident state machine: `new` / `ongoing` / `resolved` notification
  lifecycle with throttling.
- Webhook `Sink` (Slack-shaped and generic JSON) with retry/backoff and a
  local-file fallback.
- Heartbeat / dead-man's-switch.

**Out of scope (deferred to later milestones)**

- Persistent incident history surviving restarts and the interactive TUI.
- Scoring against external ground truth.

Requirement IDs use the form `PROD-<AREA>-<n>`. Requirements build on the
interfaces defined in `../01-core-engine/requirements.md`.

## 3. Functional requirements

### 3.1 Docker source

- **PROD-SRC-1** — WHEN configured with one or more container selectors (by
  name, by label, or "all running"), THE SYSTEM SHALL stream those containers'
  logs through the Docker Engine API over the Docker socket, without requiring
  the `docker` CLI binary.
- **PROD-SRC-2** — WHEN a container's log stream is multiplexed (non-TTY
  containers), THE SYSTEM SHALL demultiplex it using the Docker frame header
  format and split frames into individual log lines, carrying partial lines
  across frame boundaries.
- **PROD-SRC-3** — THE SYSTEM SHALL preserve the stdout/stderr distinction of
  each line where the API provides it.
- **PROD-SRC-4** — WHEN a selected container starts or restarts after Watcher
  has begun running, THE SYSTEM SHALL begin streaming its logs without operator
  intervention.
- **PROD-SRC-5** — WHEN a selected container stops, THE SYSTEM SHALL end that
  stream and continue serving the remaining containers.
- **PROD-SRC-6** — THE SYSTEM SHALL tag every line with a container identity
  (name and short ID) usable as the incident `source` field.
- **PROD-SRC-7** — THE SYSTEM SHALL NOT stream logs from its own container,
  extending the milestone-01 guardrail to container identity.
- **PROD-SRC-8** — WHEN the Docker socket is missing, inaccessible, or
  returns an authorization error, THE SYSTEM SHALL fail at startup with a clear
  stderr diagnostic and a non-zero exit, rather than idling silently.
- **PROD-SRC-9** — THE SYSTEM SHALL support a configurable lookback window so
  that, on startup, it may begin from a bounded amount of existing container
  log text rather than only from new output.

### 3.2 Docker Compose packaging

- **PROD-PKG-1** — The repository SHALL ship a Docker Compose file defining a
  `watcher` service and an `ollama` service on a shared network.
- **PROD-PKG-2** — THE SYSTEM SHALL not begin serving explanations until the
  `ollama` service reports healthy.
- **PROD-PKG-3** — WHEN the stack starts with the configured model absent from
  the model store, THE SYSTEM SHALL pull that model automatically on first boot
  without manual steps.
- **PROD-PKG-4** — Model weights SHALL persist in a named volume so they are
  not re-pulled on subsequent boots.
- **PROD-PKG-5** — The `watcher` service SHALL mount the Docker socket
  read-only and only the volumes required to read target logs.
- **PROD-PKG-6** — Model name, webhook URL, heartbeat URL, and timing windows
  SHALL be settable through environment variables in the Compose file.
- **PROD-PKG-7** — The `watcher` service SHALL restart automatically on
  failure, and SHALL continue to operate (detecting and reporting) when the
  model backend is temporarily unavailable.

### 3.3 Incident state machine

- **PROD-STM-1** — WHEN a fingerprint is observed for the first time, THE
  SYSTEM SHALL emit a `new` notification immediately, attaching the explanation
  once it is available.
- **PROD-STM-2** — WHEN a known fingerprint recurs, THE SYSTEM SHALL NOT emit a
  notification per occurrence. It SHALL suppress them and instead emit at most
  one `ongoing` notification per throttle window, reporting the current
  cumulative count.
- **PROD-STM-3** — WHEN no occurrence of a fingerprint has been observed for
  the resolve window, THE SYSTEM SHALL emit exactly one `resolved`
  notification carrying the final count.
- **PROD-STM-4** — WHEN a fingerprint recurs after being resolved, THE SYSTEM
  SHALL begin a fresh notification cycle and emit a `new` notification.
- **PROD-STM-5** — THE SYSTEM SHALL record every occurrence for counting
  purposes even when its notification is suppressed.
- **PROD-STM-6** — The throttle window and resolve window SHALL both be
  configurable.
- **PROD-STM-7** — WHEN the daemon restarts, THE SYSTEM SHALL NOT emit spurious
  `resolved` notifications for fingerprints it no longer remembers.
- **PROD-STM-8** — WHEN an explanation is still in flight at notification time,
  THE SYSTEM SHALL emit the notification with an explicit pending marker rather
  than waiting indefinitely for the model.

### 3.4 Webhook sink

- **PROD-WH-1** — THE SYSTEM SHALL deliver notifications to a configurable
  HTTP(S) webhook URL.
- **PROD-WH-2** — THE SYSTEM SHALL support two payload shapes, selectable by
  configuration: a Slack incoming-webhook-compatible payload, and a generic
  JSON payload.
- **PROD-WH-3** — Webhook delivery SHALL be gated by the state machine: only
  `new`, `ongoing`, and `resolved` notifications are delivered.
- **PROD-WH-4** — WHEN a delivery attempt fails (non-2xx, timeout, connection
  error), THE SYSTEM SHALL retry with exponential backoff up to a configured
  maximum number of attempts.
- **PROD-WH-5** — WHEN delivery still fails after the final attempt, THE SYSTEM
  SHALL write the notification to a local fallback file (one JSON object per
  line, including the failure reason) and continue running.
- **PROD-WH-6** — Webhook delivery SHALL NOT block the detection pipeline; it
  SHALL use a bounded queue with a documented drop policy.
- **PROD-WH-7** — Every payload SHALL contain at least: event kind, fingerprint,
  detector kind, source, severity, count, first-seen, last-seen, summary,
  likely cause, suggested fix, and confidence — with unavailable explanation
  fields represented explicitly rather than omitted silently.
- **PROD-WH-8** — WHEN multiple sinks are configured, THE SYSTEM SHALL fan out
  each notification to every sink independently, so one failing sink does not
  suppress delivery to the others.
- **PROD-WH-9** — THE SYSTEM SHALL cap the length of log-derived text placed in
  payloads so a very long line cannot produce an undeliverable request body.

### 3.5 Heartbeat

- **PROD-HB-1** — WHEN a heartbeat URL is configured, THE SYSTEM SHALL send a
  heartbeat to it periodically at a configurable interval.
- **PROD-HB-2** — The heartbeat SHALL carry at least a timestamp and current
  liveness counters (e.g. lines processed, incidents tracked, notifications
  sent), so a receiver can distinguish "alive and idle" from "dead".
- **PROD-HB-3** — THE SYSTEM SHALL run the heartbeat on its own schedule,
  independent of incident traffic.
- **PROD-HB-4** — WHEN a heartbeat delivery fails, THE SYSTEM SHALL log it and
  continue; it SHALL NOT terminate the daemon or retry indefinitely.
- **PROD-HB-5** — The heartbeat SHALL be disabled unless a URL is configured.

## 4. Non-functional requirements

- **PROD-NFR-1** — THE SYSTEM SHALL continue detecting and counting incidents
  when the model backend or any sink is unavailable.
- **PROD-NFR-2** — THE SYSTEM SHALL bound memory for container stream buffers,
  notification queues, and the fallback path.
- **PROD-NFR-3** — THE SYSTEM SHALL reconnect to Docker streams after transient
  socket errors without operator intervention, with backoff.
- **PROD-NFR-4** — All timings, endpoints, and the model name SHALL be
  configurable via flags and environment variables, consistent with the
  precedence documented in `../01-core-engine/design.md` §12.
- **PROD-NFR-5** — THE SYSTEM SHALL emit startup diagnostics (sources attached,
  sinks configured, model selected) to stderr so operators can verify
  configuration without attaching a UI.

## 5. Traceability

Design rationale for each mechanism is in [`design.md`](./design.md); task
breakdown is in [`tasks.md`](./tasks.md).
