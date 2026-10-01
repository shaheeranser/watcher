# 02 — Production Shape: Requirements

## 1. Purpose

Turn the runnable daemon into something that deploys and behaves well
unattended: ship as a Compose stack with Ollama, stop spamming an operator when
something crash-loops, and make Watcher's own failure visible.

This milestone builds on `../01b-runtime/requirements.md`. The Docker
container-log source and the webhook sink it used to own now live in **01b**;
this milestone consumes them and adds the *policy* — when a notification is
worth sending — plus packaging and a heartbeat.

## 2. Scope

**In scope**

- Docker Compose packaging of `watcher` + `ollama` with model auto-pull.
- The incident state machine: `new` / `ongoing` / `resolved` notification
  lifecycle with throttling, replacing 01b's minimal throttle.
- Heartbeat / dead-man's-switch.

**Out of scope (deferred)**

- Docker container-log source and webhook sink — **implemented in 01b**.
- Install, onboarding, config file, and systemd unit — milestone 05.
- Persistent incident history and the interactive TUI — milestone 03.
- Scoring against external ground truth — milestone 04.

Requirement IDs use the form `PROD-<AREA>-<n>`. Requirements build on the
interfaces defined in `../01-core-engine/requirements.md` and extended by
`../01b-runtime/requirements.md`.

## 3. Functional requirements

### 3.1 Docker Compose packaging

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

### 3.2 Incident state machine

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
- **PROD-STM-9** — This state machine SHALL replace 01b's minimal
  per-incident notification throttle; once it is active, that throttle SHALL no
  longer gate delivery independently.

### 3.3 Heartbeat

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
- **PROD-NFR-2** — THE SYSTEM SHALL bound memory for the notification queue and
  the heartbeat path.
- **PROD-NFR-3** — All timings, endpoints, and the model name SHALL be
  configurable via flags and environment variables, consistent with the
  precedence documented in `../01-core-engine/design.md` §12 and extended by
  `../01b-runtime/design.md` §7.
- **PROD-NFR-4** — THE SYSTEM SHALL emit startup diagnostics (sinks configured,
  model selected, state-machine windows) to stderr so operators can verify
  configuration without attaching a UI.

## 5. Traceability

Design rationale for each mechanism is in [`design.md`](./design.md); task
breakdown is in [`tasks.md`](./tasks.md).
