# 01 — Core Engine: Requirements

## 1. Purpose

Deliver a single Go binary, `watcher`, that continuously reads a log stream,
notices crash/error events on its own, deduplicates them by fingerprint, curates
a small context window around each event, asks a local Ollama-hosted model to
explain the root cause, and reports the result — without a human invoking it
against a specific incident.

This milestone establishes the interfaces (`Source`, `Detector`, `Backend`,
`Sink`) and the pipeline that every later milestone extends.

## 2. Scope

**In scope**

- stdin and single-file-tail sources.
- Detection of Go panics, Python tracebacks, and generic fatal/exception lines.
- Fingerprinting (normalize + hash) with per-fingerprint count, first-seen,
  last-seen tracking for the process lifetime.
- Bounded context-window curation and multi-line block reassembly.
- HTTP backend for a local Ollama instance returning structured explanation
  fields.
- Self-watch guardrail.
- TTY vs. JSON-lines output.

**Out of scope (deferred to later milestones)**

- Docker/container sources and container packaging.
- Notification lifecycle (throttling, "still happening", "resolved") and
  webhook delivery.
- Persistent incident history and the interactive TUI.
- Scoring against ground truth.

Requirement IDs use the form `CORE-<AREA>-<n>` and are referenced from
`tasks.md`.

## 3. Definitions

- **Line** — one line of log text plus its source identity and arrival time.
- **Event** — a detected crash/error: the trigger line plus the multi-line
  block that belongs to it, and the detector kind.
- **Fingerprint** — a stable hash of a *normalized* event block, used to
  identify "the same crash happening again".
- **Excerpt** — the bounded, verbatim slice of log text sent to the model.
- **Explanation** — the structured model result (summary, likely cause,
  evidence, suggested fix, confidence, severity).

## 4. Functional requirements

### 4.1 Source

- **CORE-SRC-1** — WHEN log data is piped to stdin, THE SYSTEM SHALL read it as
  a line-delimited stream and process each line as soon as it is available,
  without waiting for EOF.
- **CORE-SRC-2** — WHEN configured with a single file path, THE SYSTEM SHALL
  tail that file, emitting lines appended after startup and not re-reading
  pre-existing content by default.
- **CORE-SRC-3** — WHEN the tailed file is truncated or replaced (log
  rotation), THE SYSTEM SHALL detect the change and continue reading from the
  new file rather than silently stalling.
- **CORE-SRC-4** — THE SYSTEM SHALL bound its in-memory buffering of source
  lines; a slow consumer SHALL apply backpressure or a documented drop policy
  rather than grow memory without limit.
- **CORE-SRC-5** — WHEN the source is exhausted (stdin EOF) or cancelled, THE
  SYSTEM SHALL flush any pending detection work, close cleanly, and exit 0.
- **CORE-SRC-6** — THE SYSTEM SHALL support very long lines (at least 64 KiB per
  line) without truncating mid-line or panicking.

### 4.2 Detector

- **CORE-DET-1** — THE SYSTEM SHALL detect Go panic blocks: a line matching
  `^panic:` followed by the `goroutine … [running]:` header and stack frames of
  the form `\t<file>.go:<line> +0x<offset>`.
- **CORE-DET-2** — THE SYSTEM SHALL detect Python tracebacks: a
  `Traceback (most recent call last):` header through the terminating
  `^\w*(Error|Exception): …` line.
- **CORE-DET-3** — THE SYSTEM SHALL detect generic fatal/exception lines
  containing severity or exception keywords (e.g. `FATAL`, `CRITICAL`, a
  `…Exception:` prefix) as a heuristic fallback, including indented
  continuation lines.
- **CORE-DET-4** — WHEN a pattern matches, THE SYSTEM SHALL assemble the full
  multi-line block using an explicit continuation rule set (indentation,
  known terminator lines, or a blank line), not just the matching line.
- **CORE-DET-5** — WHEN no pattern matches, THE SYSTEM SHALL emit no event for
  that line and continue.
- **CORE-DET-6** — WHEN an assembled block exceeds a configured maximum line
  count, THE SYSTEM SHALL truncate it deterministically, preserving the trigger
  line, the root exception line, and the outermost and innermost frames.
- **CORE-DET-7** — Detection patterns and block-assembly rules SHALL be defined
  in one place and covered by table-driven tests with golden log fixtures.
- **CORE-DET-8** — THE SYSTEM SHALL tag each event with the detector kind that
  produced it (e.g. `go-panic`, `python-traceback`, `generic-fatal`).

### 4.3 Fingerprinting

- **CORE-FP-1** — WHEN an event is detected, THE SYSTEM SHALL compute a
  fingerprint by normalizing the block and hashing the normalized text.
- **CORE-FP-2** — Normalization SHALL replace volatile tokens with stable
  placeholders, at minimum: hexadecimal addresses and offsets, UUIDs,
  timestamps, request/trace/span IDs, goroutine identifiers, PIDs, durations,
  temporary file names, and ports.
- **CORE-FP-3** — Two occurrences of the same logical crash that differ only in
  volatile tokens SHALL produce the same fingerprint.
- **CORE-FP-4** — Two crashes with different root causes SHALL produce
  different fingerprints; the fingerprint SHALL incorporate the detector kind
  and the normalized message and stack frames, not only the exception type.
- **CORE-FP-5** — THE SYSTEM SHALL track, per fingerprint, at least: occurrence
  count, first-seen time, and last-seen time, for the lifetime of the process.
- **CORE-FP-6** — THE SYSTEM SHALL emit an explanation for a given fingerprint
  at most once per configured explanation window; repeated occurrences SHALL
  update the counters without re-invoking the model.
- **CORE-FP-7** — Fingerprint normalization SHALL be covered by property-based
  tests (in addition to example-based tests).

### 4.4 Context curation

- **CORE-CTX-1** — WHEN an event is detected, THE SYSTEM SHALL build the model
  input from the matched block plus up to N preceding and M following lines,
  where N and M are configurable.
- **CORE-CTX-2** — THE SYSTEM SHALL reassemble a multi-line stack trace (or
  traceback) into one coherent block even when the source interleaves unrelated
  lines, so the model never receives a spliced-together pseudo-stack.
- **CORE-CTX-3** — THE SYSTEM SHALL cap the excerpt to a configurable
  byte/token budget appropriate for a small model, truncating deterministically
  when over budget while retaining the trigger line, the root exception line,
  and the top stack frames.
- **CORE-CTX-4** — The excerpt SHALL preserve the original log text verbatim
  (no fingerprint-style normalization), so that quoted evidence can be checked
  against it.

### 4.5 Backend (Ollama)

- **CORE-BE-1** — THE SYSTEM SHALL send the excerpt to a local Ollama HTTP API
  at a configurable base URL (default `http://localhost:11434`).
- **CORE-BE-2** — THE SYSTEM SHALL allow the model name to be configured.
- **CORE-BE-3** — THE SYSTEM SHALL request structured output containing exactly
  these fields: `summary`, `likely_cause`, `evidence` (array of strings),
  `suggested_fix`, `confidence` (number in `[0,1]`), and `severity` (one of
  `low`, `medium`, `high`, `critical`).
- **CORE-BE-4** — WHEN the model returns output that cannot be parsed into the
  required schema, THE SYSTEM SHALL retry up to a configured number of attempts
  and then report the incident with an explicit "explanation unavailable"
  marker instead of crashing or emitting partial garbage.
- **CORE-BE-5** — Every entry in `evidence` SHALL be a verbatim substring of the
  excerpt. Entries that are not SHALL be dropped; WHEN any entry was dropped,
  THE SYSTEM SHALL mark the explanation as low confidence.
- **CORE-BE-6** — WHEN Ollama is unreachable or times out, THE SYSTEM SHALL
  fail that explanation gracefully (bounded retry/backoff) and continue
  processing subsequent events.
- **CORE-BE-7** — THE SYSTEM SHALL limit concurrent backend requests to a
  configurable worker pool (default 1), so a single small model is not flooded.
- **CORE-BE-8** — THE SYSTEM SHALL apply a configurable per-request timeout so a
  hung model call cannot stall the pipeline indefinitely.
- **CORE-BE-9** — THE SYSTEM SHALL bound the number of tokens the model may
  generate per request with a configurable cap, so a model that loops cannot
  generate until the per-request timeout on every attempt.

### 4.6 Guardrail

- **CORE-GRD-1** — THE SYSTEM SHALL NOT watch its own process's output.
  Specifically it SHALL refuse to attach to a source it identifies as its own,
  using at least: same inode as its own stdout/stderr (Linux `/proc/self/fd`),
  the source's writer PID equal to its own PID, or a container ID equal to its
  own.
- **CORE-GRD-2** — WHEN the guardrail triggers, THE SYSTEM SHALL write a clear
  diagnostic to stderr and exit non-zero rather than entering a feedback loop.
- **CORE-GRD-3** — THE SYSTEM SHALL write all of its own diagnostics to stderr
  only, so a `watcher … | watcher` pipeline cannot self-feed through stdout.

### 4.7 Output

- **CORE-OUT-1** — WHEN stdout is a terminal
  (`golang.org/x/term.IsTerminal`), THE SYSTEM SHALL render each incident as a
  human-readable structured result (summary, likely cause, severity, evidence,
  suggested fix, count, first/last seen).
- **CORE-OUT-2** — WHEN stdout is not a terminal, THE SYSTEM SHALL emit one
  JSON object per line (JSON Lines) with stable field names.
- **CORE-OUT-3** — The JSON object SHALL include at least: `fingerprint`,
  `kind`, `source`, `count`, `first_seen`, `last_seen`, `summary`,
  `likely_cause`, `evidence`, `suggested_fix`, `confidence`, `severity`, and
  `model`.
- **CORE-OUT-4** — THE SYSTEM SHALL write nothing to stdout except the result
  stream (TTY text or JSON lines), so machine consumers see clean output.
- **CORE-OUT-5** — THE SYSTEM SHALL exit non-zero on fatal startup errors
  (bad config, guardrail trip, unrecoverable source error) and 0 on clean
  shutdown.

## 5. Non-functional requirements

- **CORE-NFR-1** — THE SYSTEM SHALL shut down gracefully on `SIGINT`/`SIGTERM`,
  flushing in-flight work and closing sources without leaking goroutines.
- **CORE-NFR-2** — THE SYSTEM SHALL be configurable via command-line flags and
  environment variables with a documented precedence.
- **CORE-NFR-3** — THE SYSTEM SHALL keep memory bounded (a ring buffer of
  recent lines for context curation) under sustained input.
- **CORE-NFR-4** — THE SYSTEM SHALL not block the source read path on a slow
  model call; source-to-detection latency SHALL be independent of model
  latency.
- **CORE-NFR-5** — THE SYSTEM SHALL run as a single static binary with no cgo
  requirement in this milestone.

## 6. Traceability

Each functional requirement above is covered by one or more tasks in
`tasks.md`. The rationale behind the chosen mechanisms (normalization rules,
block assembly, prompt shape, concurrency model) is in `design.md`.
