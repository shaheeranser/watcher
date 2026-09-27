# Contributing

Thanks for taking the time to contribute. This document covers how to build and
test Watcher locally, the code style we expect, and how to get a change merged.

## Before you open a PR

**Open an issue first for anything non-trivial.** A new source, a new detector,
a change to the fingerprint normalization rules, or anything touching an
interface boundary should be discussed in an issue before code is written. This
saves you from building something that conflicts with the design in `specs/`.

Small, obviously-correct fixes (typos, a clear bug with an obvious fix, a
missing test for existing behavior) can go straight to a PR.

## Building and testing

Watcher is a Go project. You need Go 1.26 or newer.

```sh
go build ./...
go vet ./...
go test ./...
```

All three must pass. CI runs exactly these commands on every push and pull
request.

Useful while iterating:

```sh
go test ./internal/...        # unit tests only
go test -run TestNormalize ./internal/fingerprint/ -v
go build -o bin/watcher ./cmd/watcher
```

## Code style

The project-wide rules for file structure and comments are in
[`specs/style-guide.md`](./specs/style-guide.md): one concern per file, comments
that explain intent rather than mechanics, and no comment banners. How work is
done — spec-first, open decisions, commit hygiene, the per-run reports — is in
[`specs/agent-practices.md`](./specs/agent-practices.md), which applies to human
contributors as much as to agents.

- **`gofmt` is mandatory.** Run `gofmt -w .` (or let your editor do it) and
  check with `gofmt -l .`, which must print nothing.
- Follow the conventions already in the codebase rather than introducing new
  ones. Prefer table-driven tests, wrap errors with `%w` and context, and keep
  package-private types unexported unless a sibling package genuinely needs
  them.
- Do not add a dependency for something the standard library already does.
- The binary must keep building with `CGO_ENABLED=0`.

Before starting on a milestone, read its design document in `specs/` — for
example `specs/01-core-engine/design.md` — since it records *why* the
interfaces are shaped the way they are, and the reasoning is usually the
non-obvious part.

## Tests

- New behavior needs tests. Bug fixes need a regression test that fails before
  the fix.
- Property-based tests are expected for the fingerprint normalization logic:
  idempotence, invariance to volatile tokens, distinctness, and determinism.
- Tests must run offline. Do not require a live Ollama instance — stand in for
  it with `httptest`.

## Submitting a pull request

1. Fork the repository and branch from `main`.
2. Make your change, including tests and any spec updates it implies.
3. Run `go build ./...`, `go vet ./...`, `go test ./...`, and `gofmt -l .`.
4. Open the PR and fill in the checklist. Link the issue it addresses.
5. Keep the PR focused. Unrelated refactors belong in their own PR.

A maintainer will review, may ask for changes, and will merge when it is ready.

## What gets closed

Low-effort and spam pull requests will be closed rather than merged. That
includes PRs that only reformat files without a stated reason, drive-by
dependency bumps with no explanation, changes that add no tests for new
behavior, and anything that ignores the design in `specs/`. This is not
personal — it keeps the review queue usable. If in doubt, open an issue and ask
first.

## License

By contributing, you agree that your contributions are licensed under the MIT
License (see [LICENSE](./LICENSE)).
