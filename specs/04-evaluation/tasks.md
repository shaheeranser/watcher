# 04 — Evaluation: Tasks

Each task notes the requirements it satisfies. Read
[`design.md`](./design.md) before starting, plus
`../01-core-engine/design.md` §10 for the JSONL output contract being
consumed.

## 1. Ground-truth input

- [x] **T-1.1** — Define the ground-truth model (identifier, expected cause,
      optional expected kind). *(EVAL-GT-2)*
- [x] **T-1.2** — Implement the JSON reader for both the bare-object and
      `cases`-array shapes. *(EVAL-GT-1)*
- [x] **T-1.3** — Implement the CSV reader with header detection and quoted
      fields. *(EVAL-GT-1)*
- [x] **T-1.4** — Implement format detection by extension with an override flag.
      *(EVAL-GT-1)*
- [x] **T-1.5** — Implement validation: required non-empty fields, duplicate
      identifiers, empty file. *(EVAL-GT-4, EVAL-GT-5)*
- [x] **T-1.6** — Document the format so the harness repo can be written against
      it without reading Watcher source. *(EVAL-GT-3, EVAL-NFR-3)*
- [x] **T-1.7** — Tests: both JSON shapes, CSV with quoting/commas, empty,
      duplicates, missing fields, unicode.

## 2. Actual results input

- [x] **T-2.1** — Implement the streaming JSONL reader from a file or stdin.
      *(EVAL-ACT-1, EVAL-ACT-2, EVAL-NFR-2)*
- [x] **T-2.2** — Ignore unknown fields and skip blank/junk lines while
      counting them. *(EVAL-ACT-3)*
- [x] **T-2.3** — Detect and report the empty/nothing-parseable case distinctly.
      *(EVAL-ACT-4)*
- [x] **T-2.4** — Add the additive `run_id` field to the emitted result when a
      run id is configured on the producing invocation. *(EVAL-MATCH-2)*
- [x] **T-2.5** — Tests: valid stream, junk lines, unknown fields, empty input,
      stdin pipeline.

## 3. Matching

- [x] **T-3.1** — Implement fingerprint-mode correlation. *(EVAL-MATCH-1)*
- [x] **T-3.2** — Implement run-id-mode correlation, including accepting the run
      id from env/flag and echoing it in output. *(EVAL-MATCH-1, EVAL-MATCH-2)*
- [x] **T-3.3** — Implement truth-only reporting ("no result produced").
      *(EVAL-MATCH-3)*
- [x] **T-3.4** — Implement result-only reporting (extra/unscored).
      *(EVAL-MATCH-4)*
- [x] **T-3.5** — Implement the deterministic multi-result tie-break.
      *(EVAL-MATCH-5, OD-04-4)*
- [x] **T-3.6** — Tests for both modes and all unmatched directions.

## 4. Scoring

- [x] **T-4.1** — Implement cause normalization (NFKC, lowercase, strip
      punctuation, collapse whitespace, stopwords). *(EVAL-SCORE-2)*
- [x] **T-4.2** — Implement token-set (Dice) similarity. *(EVAL-SCORE-3)*
- [x] **T-4.3** — Implement character-trigram similarity and take the max.
      *(EVAL-SCORE-3)*
- [x] **T-4.4** — Implement exact-normalized-match short-circuit to a pass.
      *(EVAL-SCORE-3)*
- [x] **T-4.5** — Implement the per-pair pass/fail classification and the
      configurable threshold governing it. *(EVAL-SCORE-1, EVAL-SCORE-3,
      OD-04-1)*
- [x] **T-4.6** — Record the raw score per case independently of the verdict, and
      make re-thresholding a stored run possible. *(EVAL-SCORE-4)*
- [x] **T-4.7** — Implement the `no_explanation` category from the
      explanation-unavailable result. *(EVAL-SCORE-5)*
- [x] **T-4.8** — Implement expected-kind comparison and the `kind_mismatch`
      category, gated by `--require-kind`. *(EVAL-SCORE-6, OD-04-5)*
- [x] **T-4.9** — Assert determinism in tests (same inputs → same scores).
      *(EVAL-SCORE-7)*
- [x] **T-4.10** — Assert no network access in the scoring path.
      *(EVAL-SCORE-8, EVAL-NFR-1)*
- [x] **T-4.11** — Tests: exact, word-order variation, morphological variation,
      genuinely different causes, and boundaries at `τ`.

## 5. Reports and CLI

- [x] **T-5.1** — Implement the `watcher eval` command and flag set from
      `design.md` §6.
- [x] **T-5.2** — Implement the text tally (totals, pass rate, gate verdict).
      *(EVAL-OUT-1, EVAL-OUT-2)*
- [x] **T-5.3** — Implement the failure list with expected vs. actual, score, and
      category. *(EVAL-OUT-3)*
- [x] **T-5.4** — Implement the by-detector-kind breakdown including
      `no_explanation`. *(EVAL-OUT-4)*
- [x] **T-5.5** — Implement the JSON report with per-case records.
      *(EVAL-OUT-5)*
- [x] **T-5.6** — Implement exit codes against `min_pass_rate`.
      *(EVAL-OUT-6, EVAL-OUT-7, OD-04-2)*
- [x] **T-5.7** — Implement unmatched reporting in both output formats.
      *(EVAL-OUT-8)*
- [x] **T-5.8** — Golden-file tests for both report formats; exit-code table
      tests straddling the gate.

## 6. Integration

- [x] **T-6.1** — Write the harness integration contract from `design.md` §7
      into the README/docs for the harness repo.
- [x] **T-6.2** — End-to-end: a small fixed corpus (a few logs, a truth file)
      scored from a captured results file, deterministically reproducing the
      same tally twice.
- [x] **T-6.3** — Verify a run can be re-scored at a different threshold without
      re-running Watcher or the model. *(EVAL-NFR-1)*

## 7. Exit criteria

- [x] Every `EVAL-*` requirement has a passing test.
- [x] `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` passes.
- [x] `watcher … --json | watcher eval --truth … --results -` works end to end.
- [x] The same run scored twice produces identical output.
- [x] The ground-truth format is documented well enough to write the harness
      repo against it. *(EVAL-NFR-3)*
- [x] Resolve the open decisions in `design.md` §9 and update the spec with the
      chosen values.
