# AGENTS.md

Instructions for AI coding agents working in this repository. This file is
tool-agnostic: nothing in it assumes a particular agent product, vendor, or
editor integration.

The committed, authoritative standards are
[`specs/style-guide.md`](./specs/style-guide.md) (how code should read) and
[`specs/agent-practices.md`](./specs/agent-practices.md) (how work is done);
this file is the entry point that points at them.

## Read the spec before you implement

`specs/` is the project's design documentation and is committed. Before
implementing any milestone's tasks, read that milestone's design document:

- `specs/<milestone>/requirements.md` — testable acceptance criteria (EARS-style
  "WHEN X, THE SYSTEM SHALL Y") with stable requirement IDs.
- `specs/<milestone>/design.md` — the rationale, interfaces, and failure modes.
  **Read this first.** It explains why a mechanism is shaped the way it is.
- `specs/<milestone>/tasks.md` — the checklist, with each task citing the
  requirement IDs it satisfies.

Milestones are numbered and build on each other: `00-scaffolding`,
`01-core-engine`, `02-production-shape`, `03-dashboard`, `04-evaluation`. A
later design may reference an earlier one; earlier ones never depend on later
ones, and you should preserve that direction when editing.

Do not silently resolve an open decision. Each `design.md` ends with a numbered
`OD-xx` list. If your work depends on one, surface it rather than picking a
value you cannot justify.

## Build, test, lint

```sh
go build ./...
go vet ./...
go test ./...
gofmt -l .              # must print nothing
```

CI runs `go build ./...`, `go vet ./...`, and `go test ./...` on push and pull
request. Run them yourself before considering a change done. The binary must
keep building with `CGO_ENABLED=0`.

## Go code style

Project-wide rules for file structure and comments are in
[`specs/style-guide.md`](./specs/style-guide.md); the bullets below are the
Go-specific expectations.

- `gofmt`-clean, no exceptions.
- Wrap errors with `%w` and enough context to locate the failure; do not
  discard the original error.
- No `panic` outside `main` and init-time programming errors; a log line must
  never take down the daemon.
- Keep types unexported unless a sibling package genuinely needs them. Prefer
  small interfaces — the four below are the spine and each stays narrow.
- Table-driven tests by default. Golden fixtures for anything parsing log text.
- No dependency where the standard library suffices.

## Package layout

```
cmd/watcher/            main: flags/env, wiring, signal handling
internal/config/        configuration, defaults, precedence, validation
internal/source/        Source interface; stdin, file tail, docker
internal/detect/        Detector; trigger patterns, block assembly
internal/fingerprint/   normalization rules + hashing
internal/context/       bounded window extraction, trace reassembly, budget
internal/incident/      counts, state machine, notification decisions, store
internal/backend/       Backend interface; Ollama client, prompt, schema
internal/sink/          Sink interface; terminal, jsonl, webhook
internal/guard/         self-watch guardrail
internal/engine/        pipeline wiring: goroutines, channels, shutdown
```

The authoritative version, with the reasoning, is
`specs/01-core-engine/design.md` §3; later milestones add implementations under
these packages rather than new top-level shapes.

## Interface boundaries you must respect

Four interfaces carry the system. Keep them separate — do not collapse one into
another, and do not let a new feature reach across them:

- **`Source`** produces log lines. It must not know about crashes, models,
  fingerprints, or sinks.
- **`Detector`** consumes lines and emits crash events. It must not know about
  models or sinks.
- **`Backend`** takes a curated excerpt and returns a structured explanation. It
  must not know about sources, sinks, or notification policy.
- **`Sink`** delivers a finished result. It must not know about detection.

Fingerprinting, context curation, and incident tracking sit *between*
`Detector` and `Backend`. That placement is the point: it is what keeps the
small model's input clean and its call rate low.

Two invariants worth stating explicitly, because they are easy to break:

1. **Watcher must never watch its own process output.** The guardrail in
   `internal/guard` is load-bearing, not a nicety. Do not bypass it.
2. **stdout carries only results** (TTY text or JSON lines). All diagnostics go
   to stderr, so a `watcher … | watcher` pipeline cannot feed itself.

## Testing expectations

- Property-based tests are **expected** for the fingerprint normalization logic:
  idempotence, invariance to volatile tokens (addresses, timestamps, UUIDs,
  request IDs), distinctness across root causes, and determinism. See
  `specs/01-core-engine/design.md` §6.
- Table-driven tests over `testdata/` golden fixtures for detectors.
- Everything runs offline. Do not require a live Ollama server or Docker daemon
  in unit tests — use `httptest` and fakes. Integration tests that do need those
  must be clearly separated and skippable.

## Commit hygiene

Do not add `Co-authored-by` trailers for agents or tooling. Commits should
reflect the humans responsible for the change.
