# Agent practices

How work is done in this repository. These are project rules, not tool or
environment settings: they say what this codebase expects of anyone working on
it, and they are written for AI coding agents because that is often who is
implementing a milestone.

Normative code shape lives in [`style-guide.md`](./style-guide.md); the design
rationale lives in each milestone's `design.md`. Anything specific to one
person's machine or agent tooling — where a tool may write files, which editor
or model is used, local scratch locations — is not a project rule and does not
belong here.

## 1. Read the spec before you implement

Each milestone in this directory is the design record: `requirements.md`
(testable acceptance criteria with stable IDs), `design.md` (rationale,
interfaces, failure modes — read this first), and `tasks.md` (the checklist,
each task citing the requirement IDs it satisfies). Implement against the spec,
not against a guess about what the feature should be.

Milestones build in order: a later `design.md` may reference an earlier one, but
an earlier one never depends on a later one. Preserve that direction when
editing.

## 2. Do not silently resolve an open decision

Every `design.md` ends with a numbered `OD-xx` list. These are choices the
project owner has reserved. Surface them — ask — rather than picking a value
you cannot justify. When one *is* decided, record the resolution and its
reasoning in the design, so the next reader does not have to rediscover it.

## 3. Ask when implementation demands it

If a task needs a decision the spec does not make, ask. Guessing produces work
that has to be undone, and buries a design choice inside an implementation.

## 4. Commits

- Small, focused commits made along the way, not one large commit at the end.
- Labelled, human-readable messages — `feat`, `fix`, `chore`, `docs`, `test` —
  with an optional scope, e.g. `feat(detect): …`.
- One concern per commit, and a commit should build and pass tests on its own.
- No `Co-authored-by` trailers for agents or tooling. Commits reflect the
  humans responsible for the change.

## 5. Every run leaves a report

A development run is not finished until its report exists:

- `docs/dev-runs/<run>.md` — what was built, how it is shaped, how to run and
  test it, how to validate it on a real failure, the commit walkthrough, and
  the known gaps. The required sections are defined in
  `docs/dev-runs/README.md`.
- `docs/victim/<run>.md` — concrete suggested changes to the external
  evaluation harness (`watcher_victim`), based on what the run changed.

These live under `docs/`, which is intentionally untracked: they are working
records, while `specs/` is the committed, normative documentation.

## 6. Validate against a real failure

Green unit tests are necessary but not sufficient. A run is validated by driving
the feature end to end on a genuine error with the real dependencies — a real
crash, a real model, or the ground-truth harness — and checking the result
against expected behavior. See the "validating against a real failure" section
of a run report for the procedure.

## 7. Report status honestly

The README describes the product's *full intended scope*, including capabilities
that later milestones will deliver; it is not softened to match what is
implemented today. What exists versus what is still a gap belongs in the run
report's "known gaps". Never describe a capability as achieved when a later
milestone owns it.

## 8. Change as little as the spec requires

Make the smallest change that satisfies the requirement. Do not add features,
configurability, or abstractions the task did not ask for, and do not pre-build
for hypothetical future milestones. Delete code that is no longer used rather
than leaving compatibility shims. The milestone that needs something adds it.

## 9. Respect the interface boundaries

Four interfaces carry the system — `Source`, `Detector`, `Backend`, `Sink` —
and they stay separate. Fingerprinting, context curation, and incident tracking
sit *between* `Detector` and `Backend`; that placement is what keeps the model's
input clean and its call rate low. Do not collapse one interface into another,
and do not let a feature reach across them. The authoritative description is
[`01-core-engine/design.md`](./01-core-engine/design.md) §3–4.

Two invariants are load-bearing and must not be bypassed:

1. Watcher must never watch its own process output. The guard in
   `internal/guard` is not a nicety.
2. stdout carries only results (TTY text or JSON Lines); all diagnostics go to
   stderr, so a `watcher … | watcher` pipeline cannot feed itself.

## 10. Leave the build green

Before considering work done: `gofmt -l .` prints nothing, and `go build ./...`,
`go vet ./...`, and `go test ./...` all pass. The binary must keep building with
`CGO_ENABLED=0`.
