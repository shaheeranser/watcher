# 03 — Dashboard: Requirements

## 1. Purpose

Give operators a way to inspect what Watcher has noticed without turning the
daemon into a UI process. The daemon stays headless and unattended; a separate
`watcher attach` command connects to it and renders a live terminal UI. Incident
history persists across restarts so a crash-looping service is still visible
after Watcher itself is restarted.

## 2. Scope

**In scope**

- A read API served by the always-headless daemon over a local endpoint.
- A `watcher attach` command rendering a live TUI (bubbletea/lipgloss).
- A two-pane layout: grouped-by-fingerprint incident list, detail pane below.
- Live refresh without manual intervention.
- SQLite-backed incident history that survives restarts.

**Out of scope (deferred to a later milestone)**

- Scoring against external ground truth.

Acknowledging, silencing, or muting incidents from the UI is **out of scope for
this milestone** (the UI is read-only); see OD-03-7.

Requirement IDs use the form `DASH-<AREA>-<n>`.

## 3. Functional requirements

### 3.1 Headless daemon and endpoint

- **DASH-1** — THE SYSTEM SHALL run the watcher daemon entirely headless; it
  SHALL NOT require, allocate, or render a terminal.
- **DASH-2** — THE SYSTEM SHALL serve a local read API from the daemon,
  reachable over a local socket (Unix domain socket preferred) or loopback
  HTTP, independent of whether any client is attached.
- **DASH-3** — WHEN the daemon is running with no clients attached, THE SYSTEM
  SHALL incur no rendering work and no per-client polling cost.
- **DASH-4** — THE SYSTEM SHALL expose, over the API, at least: the incident
  list, a single incident's detail including its explanation, and recent
  occurrence timestamps.
- **DASH-5** — WHEN the API socket cannot be created because another instance
  holds it, THE SYSTEM SHALL fail with a clear diagnostic rather than silently
  binding elsewhere.
- **DASH-6** — THE SYSTEM SHALL allow the API to be disabled by configuration,
  in which case the daemon continues detecting and notifying as before.

### 3.2 Attach command

- **DASH-7** — THE SYSTEM SHALL provide a `watcher attach` command that
  connects to the running daemon's endpoint and renders a live TUI.
- **DASH-8** — WHEN no daemon is reachable, `watcher attach` SHALL exit with a
  clear, actionable error message and a non-zero status, rather than hanging or
  rendering an empty screen indefinitely.
- **DASH-9** — WHEN the daemon disconnects or restarts while attached, THE
  SYSTEM SHALL show a visibly disconnected state and reconnect automatically
  with backoff, resuming live updates without operator action.
- **DASH-10** — THE SYSTEM SHALL never require `attach` to have write access to
  incident state; the UI is read-only in this milestone.

### 3.3 TUI layout and interaction

- **DASH-11** — THE SYSTEM SHALL render a grouped-by-fingerprint incident list
  in the upper pane (one row per distinct incident/fingerprint) and a detail
  pane below for the currently selected incident.
- **DASH-12** — Each list row SHALL show at least: severity, detector kind,
  source, cumulative count, most-recent-seen age, and a truncated summary.
- **DASH-13** — THE SYSTEM SHALL show in the detail pane at least: summary,
  likely cause, verbatim evidence lines, suggested fix, confidence, state
  (`new`/`ongoing`/`resolved`), count, first-seen, and last-seen.
- **DASH-14** — THE SYSTEM SHALL support keyboard navigation: move selection up
  and down, switch focus between panes, refresh, toggle a help overlay, and
  quit.
- **DASH-15** — WHEN the terminal is resized, THE SYSTEM SHALL reflow both panes
  to the new size without panicking.
- **DASH-16** — WHEN the terminal is smaller than a usable minimum, THE SYSTEM
  SHALL render a clear "terminal too small" message instead of corrupting the
  layout.
- **DASH-17** — THE SYSTEM SHALL sort the list with unresolved incidents before
  resolved ones, and within each group by most-recent activity, so the thing
  currently on fire is at the top.
- **DASH-18** — THE SYSTEM SHALL indicate visually when an incident's
  explanation is still pending or unavailable, rather than showing blank fields.

### 3.4 Live updates

- **DASH-19** — WHEN incident state changes while attached (new incident, count
  change, state transition, explanation arrival), THE SYSTEM SHALL reflect the
  change in the UI without any manual refresh.
- **DASH-20** — The UI SHALL refresh within a bounded, configurable interval of
  the underlying change, where the bound does not depend on polling the model
  or re-running detection.
- **DASH-21** — WHEN the update channel stalls, THE SYSTEM SHALL fall back to
  periodic polling so the UI does not freeze silently.

### 3.5 Persistence

- **DASH-22** — THE SYSTEM SHALL persist incidents and their explanations to a
  local SQLite database.
- **DASH-23** — WHEN the daemon restarts, THE SYSTEM SHALL make previously
  recorded incidents (counts, first/last seen, state, explanations) available
  again through the API and UI.
- **DASH-24** — THE SYSTEM SHALL record occurrence timestamps such that the
  detail pane can show recent activity for an incident.
- **DASH-25** — THE SYSTEM SHALL bound the database's growth with a
  configurable retention policy (e.g. by age and/or per-incident occurrence
  cap).
- **DASH-26** — WHEN the database file cannot be opened or migrated, THE
  SYSTEM SHALL report the failure clearly; persistence failure SHALL NOT stop
  detection and notification from working.

## 4. Non-functional requirements

- **DASH-NFR-1** — The `attach` process SHALL remain responsive (input latency
  unaffected by API or database latency).
- **DASH-NFR-2** — The daemon SHALL NOT block detection, fingerprinting, or
  notification on UI clients or on database writes.
- **DASH-NFR-3** — Concurrent readers (one or more `attach` clients) SHALL NOT
  block the daemon's writes, and vice versa.
- **DASH-NFR-4** — The TUI SHALL degrade acceptably without color and on
  limited terminals (`TERM=dumb`, no truecolor).
- **DASH-NFR-5** — The API SHALL be versioned so the TUI and daemon can be
  upgraded independently.

## 5. Traceability

Mechanism rationale is in [`design.md`](./design.md); task breakdown is in
[`tasks.md`](./tasks.md).
