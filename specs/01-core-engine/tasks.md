# 01 — Core Engine: Tasks

Each task notes the requirements it satisfies. Read
[`design.md`](./design.md) before starting; it holds the rationale for the
mechanisms named here.

Suggested order: config → source → detector → fingerprint → context →
incident tracker → backend → sinks → guardrail → wiring → e2e.

## 1. Skeleton and configuration

- [x] **T-1.1** — Create the package layout from `design.md` §3
      (`cmd/watcher`, `internal/{config,source,detect,fingerprint,context,incident,backend,sink,guard,engine}`).
      *(scaffolding)*
- [x] **T-1.2** — Implement `internal/config`: struct, defaults, flag/env
      parsing, precedence (flag > env > default), validation. *(CORE-NFR-2)*
- [x] **T-1.3** — Wire `main`: parse config, select source and sink, start the
      engine, install `SIGINT`/`SIGTERM` handling. *(CORE-NFR-1, CORE-OUT-5)*
- [x] **T-1.4** — Verify the build produces a single static binary with
      `CGO_ENABLED=0` and no cgo dependency. *(CORE-NFR-5)*

## 2. Source

- [x] **T-2.1** — Define the `Source` interface and `Line` type
      (`design.md` §4).
- [x] **T-2.2** — Implement `stdin` source: line-delimited reads, streaming
      (no waiting for EOF). *(CORE-SRC-1)*
- [x] **T-2.3** — Test very long lines (≥64 KiB) and missing final newline.
      *(CORE-SRC-6)*
- [x] **T-2.4** — Implement single-file tail source, starting at end by
      default. *(CORE-SRC-2)*
- [x] **T-2.5** — Implement rotation/truncation handling (inode + size change).
      *(CORE-SRC-3)*
- [x] **T-2.6** — Bound buffering / define the slow-consumer policy.
      *(CORE-SRC-4)*
- [x] **T-2.7** — Implement clean source shutdown and EOF flush. *(CORE-SRC-5)*
- [x] **T-2.8** — Tests: stdin streaming, file append pickup, rotation, EOF,
      cancellation.

## 3. Detector

- [x] **T-3.1** — Define the `Detector` interface and `Event` type.
- [x] **T-3.2** — Implement `go-panic` trigger and frame-continuation predicate.
      *(CORE-DET-1, CORE-DET-8)*
- [x] **T-3.3** — Implement `python-traceback` trigger and terminator predicate.
      *(CORE-DET-2)*
- [x] **T-3.4** — Implement `generic-fatal` heuristic (FATAL/CRITICAL/Exception
      prefixes) with indented continuation. *(CORE-DET-3)*
- [x] **T-3.5** — Implement the block-assembly state machine with per-kind
      continuation predicates. *(CORE-DET-4)*
- [x] **T-3.6** — Implement max-block truncation preserving trigger, root
      exception, and outer/inner frames. *(CORE-DET-6)*
- [x] **T-3.7** — Add golden fixtures under `testdata/` and table-driven tests
      for all three kinds; assert non-matching lines emit nothing.
      *(CORE-DET-5, CORE-DET-7)*
- [x] **T-3.8** — Capture trailing tail lines for context curation.
      *(CORE-CTX-1)*

## 4. Fingerprinting

- [x] **T-4.1** — Implement the ordered rewrite rules from `design.md` §6.
      *(CORE-FP-1, CORE-FP-2)*
- [x] **T-4.2** — Implement whitespace normalization, empty-line dropping, and
      `sha256(kind + "\n" + normalized)` truncated to 16 hex chars.
      *(CORE-FP-1)*
- [x] **T-4.3** — Example tests for every rewrite rule.
- [x] **T-4.4** — Property-based tests for the four invariants: idempotence,
      volatile invariance, distinctness, determinism. *(CORE-FP-3, CORE-FP-4,
      CORE-FP-7)*
- [x] **T-4.5** — Test that two crashes differing only in addresses/UUIDs/
      timestamps collapse to one fingerprint, and that different root causes do
      not. *(CORE-FP-3, CORE-FP-4)*

## 5. Context curation

- [x] **T-5.1** — Implement the bounded ring buffer of recent lines.
      *(CORE-NFR-3)*
- [x] **T-5.2** — Implement window extraction (N before, block, M after).
      *(CORE-CTX-1)*
- [x] **T-5.3** — Implement interleaved-line reassembly with elision markers.
      *(CORE-CTX-2)*
- [x] **T-5.4** — Implement budget trimming in the specified order (middle
      frames → preceding → following; never trigger/root/top frames).
      *(CORE-CTX-3)*
- [x] **T-5.5** — Assert verbatim preservation (no normalization) in tests.
      *(CORE-CTX-4)*
- [x] **T-5.6** — Prepend source/kind header to the excerpt.

## 6. Incident tracker

- [x] **T-6.1** — Implement the process-local tracker: map fingerprint →
      count, first-seen, last-seen, last-explained-at. *(CORE-FP-5)*
- [x] **T-6.2** — Gate model calls by the explanation window; update counters
      on repeat occurrences without re-calling. *(CORE-FP-6, CORE-NFR-4)*
- [x] **T-6.3** — Tests: repeat occurrence increments count with no second
      backend call; explanation window expiry allows a fresh call.

## 7. Backend

- [x] **T-7.1** — Define the `Backend` interface, `Explanation`, and `Request`
      types.
- [x] **T-7.2** — Implement the Ollama client: `POST /api/chat`, `stream:
      false`, `temperature: 0`, configurable base URL and model.
      *(CORE-BE-1, CORE-BE-2)*
- [x] **T-7.3** — Implement the prompt template from `design.md` §8.1.
- [x] **T-7.4** — Implement schema-constrained output (`format`) with the
      client-side validator fallback. *(CORE-BE-3)*
- [x] **T-7.5** — Implement invalid-output retry (up to K) then "explanation
      unavailable" result. *(CORE-BE-4)*
- [x] **T-7.6** — Implement evidence grounding against the excerpt; drop
      ungrounded entries and floor confidence. *(CORE-BE-5)*
- [x] **T-7.7** — Implement reachability/timeout handling with bounded
      retry/backoff; never abort the pipeline. *(CORE-BE-6, CORE-BE-8)*
- [x] **T-7.8** — Implement the worker pool (default 1). *(CORE-BE-7)*
- [x] **T-7.9** — Tests with `httptest`: valid JSON, malformed JSON, 500s,
      timeouts, ungrounded evidence, and concurrent calls bounded to W.

## 8. Sinks

- [x] **T-8.1** — Define the `Sink` interface and `Result` type.
- [x] **T-8.2** — Implement the `jsonl` sink with the exact field set from
      CORE-OUT-3, RFC3339 UTC timestamps, one compact line per result.
      *(CORE-OUT-2, CORE-OUT-3)*
- [x] **T-8.3** — Implement the `terminal` sink (severity header, counts,
      times, summary, cause, evidence, fix, confidence; `NO_COLOR` respected).
      *(CORE-OUT-1)*
- [x] **T-8.4** — Select the sink by `term.IsTerminal(os.Stdout.Fd())`.
      *(CORE-OUT-1, CORE-OUT-2)*
- [x] **T-8.5** — Ensure diagnostics go only to stderr and stdout carries only
      results. *(CORE-OUT-4, CORE-GRD-3)*
- [x] **T-8.6** — Golden-file tests for both renderers (TTY and pipe).

## 9. Guardrail

- [x] **T-9.1** — Implement `guard.IsSelf`: stdout/stderr inode comparison,
      writer-PID check (Linux), container-ID hook. *(CORE-GRD-1)*
- [x] **T-9.2** — Make a positive result fatal: stderr message + non-zero exit.
      *(CORE-GRD-2)*
- [x] **T-9.3** — Tests: a source that *is* our own redirect trips the guard; an
      unrelated file does not.

## 10. Engine wiring and shutdown

- [x] **T-10.1** — Wire the pipeline goroutines and buffered channels per
      `design.md` §11.
- [x] **T-10.2** — Implement context cancellation propagation and bounded
      `WaitGroup` shutdown. *(CORE-NFR-1)*
- [x] **T-10.3** — Implement the slow-sink bounded queue and drop policy with a
      logged dropped counter. *(CORE-SRC-4)*
- [x] **T-10.4** — Goroutine-leak test on shutdown.

## 11. End-to-end and docs

- [x] **T-11.1** — E2E test: fixture log through stdin with a fake backend →
      assert JSONL output shape and content.
- [x] **T-11.2** — E2E test: repeated crash loop → one explanation, rising
      count. *(CORE-FP-6)*
- [x] **T-11.3** — E2E test: backend down → pipeline keeps processing and emits
      unavailable results. *(CORE-BE-6)*
- [x] **T-11.4** — Document the flags/env table from `design.md` §12 in the
      README quickstart.
- [x] **T-11.5** — Resolve the open decisions in `design.md` §15 and update the
      spec with the chosen values.

## 12. Exit criteria

- [x] Every functional requirement in `requirements.md` is covered by a passing
      test.
- [x] `gofmt -l .` is empty, `go vet ./...` is clean, `go test ./...` passes.
- [x] Property-based fingerprint tests exist and run in CI.
- [x] `watcher` runs unattended on a piped stream with no human interaction.
