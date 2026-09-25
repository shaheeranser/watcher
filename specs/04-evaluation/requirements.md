# 04 — Evaluation: Requirements

## 1. Purpose

Make Watcher able to score its own explanations. Given a ground-truth file that
maps an identifier to the cause a human says is correct, Watcher compares its
own output against that file and produces a pass/fail tally — suitable for a
results slide — including the cases it got wrong.

The point is measurement without coupling: the ground-truth file is a plain,
documented format that any test harness can produce, so Watcher never has to
know a particular harness's internals.

## 2. Scope

**In scope**

- Reading an external ground-truth file in JSON or CSV.
- Reading Watcher's own result output as the "actual" side.
- Matching actual results to ground-truth cases by a documented identifier.
- Comparing the actual `likely_cause` to the expected cause.
- Human-readable and machine-readable tallies, including failures.

**Out of scope**

- Producing the ground truth (that is the harness's job).
- Invoking models or running the daemon during scoring.
- Any dependence on a specific harness's file layout or IDs.

Requirement IDs use the form `EVAL-<AREA>-<n>`.

## 3. Functional requirements

### 3.1 Ground-truth input

- **EVAL-GT-1** — THE SYSTEM SHALL accept a ground-truth file in JSON or CSV,
  selected by file extension or an explicit flag.
- **EVAL-GT-2** — THE SYSTEM SHALL treat each ground-truth entry as a mapping
  from an identifier to an expected `likely_cause` (and optionally an expected
  detector kind).
- **EVAL-GT-3** — The ground-truth format SHALL be plain and fully documented,
  with no requirement that it be produced by any particular harness and no
  reference to any harness's internal structures.
- **EVAL-GT-4** — WHEN the ground-truth file is malformed, empty, or contains
  duplicate identifiers, THE SYSTEM SHALL fail with a clear, specific error and
  a non-zero exit.
- **EVAL-GT-5** — THE SYSTEM SHALL validate that every ground-truth entry has a
  non-empty identifier and a non-empty expected cause.

### 3.2 Actual results input

- **EVAL-ACT-1** — THE SYSTEM SHALL read the "actual" side from Watcher's own
  result output, i.e. the JSON-Lines incident stream defined in
  `../01-core-engine/requirements.md` (CORE-OUT-2, CORE-OUT-3).
- **EVAL-ACT-2** — THE SYSTEM SHALL accept the results from a file path or from
  stdin, so `watcher … | watcher eval …` works in a pipeline.
- **EVAL-ACT-3** — THE SYSTEM SHALL ignore non-incident lines and tolerate
  extra fields, so a results file from a newer build still parses.
- **EVAL-ACT-4** — WHEN the results input is empty or contains no parseable
  incidents, THE SYSTEM SHALL report this distinctly rather than reporting a
  0% score.

### 3.3 Matching

- **EVAL-MATCH-1** — THE SYSTEM SHALL correlate ground-truth entries with
  actual results using an identifier mode, configurable, supporting at least:
  - **fingerprint mode** — keyed on the `fingerprint` field Watcher already
    emits; and
  - **run-id mode** — keyed on an explicit run identifier supplied to the
    producing Watcher invocation and echoed in its output.
- **EVAL-MATCH-2** — In run-id mode, THE SYSTEM SHALL accept the run identifier
  from the environment or a flag on the daemon/one-shot invocation, and SHALL
  include it in every emitted result.
- **EVAL-MATCH-3** — WHEN a ground-truth entry has no corresponding result, THE
  SYSTEM SHALL report it as "no result produced" (a failure), not silently
  drop it.
- **EVAL-MATCH-4** — WHEN a result has no corresponding ground-truth entry, THE
  SYSTEM SHALL report it as extra/unscored, not as a failure.
- **EVAL-MATCH-5** — WHEN more than one result maps to the same ground-truth
  identifier, THE SYSTEM SHALL apply a documented, deterministic rule for which
  result is scored, and report the others as extra.

### 3.4 Scoring

- **EVAL-SCORE-1** — For each matched pair, THE SYSTEM SHALL compare the actual
  `likely_cause` against the expected cause and classify the case as pass or
  fail.
- **EVAL-SCORE-2** — The comparison SHALL be robust to trivial surface
  differences (case, punctuation, whitespace) via a documented normalization
  step before comparison.
- **EVAL-SCORE-3** — The comparison SHALL use a configurable similarity
  threshold; a case passes when the computed similarity meets or exceeds the
  threshold, and an exact normalized match always passes.
- **EVAL-SCORE-4** — THE SYSTEM SHALL record, per case, the computed similarity
  score and the pass/fail verdict, so results can be re-thresholded from a
  stored run without re-executing anything.
- **EVAL-SCORE-5** — WHEN a result carries no explanation (the "explanation
  unavailable" outcome from `../01-core-engine/requirements.md`, CORE-BE-4),
  THE SYSTEM SHALL count the case as a failure categorized separately as
  "no explanation".
- **EVAL-SCORE-6** — WHEN the ground-truth entry specifies an expected detector
  kind, THE SYSTEM SHALL record whether the actual kind matched, and SHALL
  report kind mismatches distinctly from cause mismatches.
- **EVAL-SCORE-7** — Scoring SHALL be deterministic: the same inputs SHALL
  produce the same verdicts and scores.
- **EVAL-SCORE-8** — Scoring SHALL perform no network access and SHALL NOT
  require the daemon, the model, or Ollama to be running.

### 3.5 Output

- **EVAL-OUT-1** — THE SYSTEM SHALL emit a pass/fail tally with the total case
  count, the pass count, the fail count, and the pass rate.
- **EVAL-OUT-2** — The human-readable tally SHALL be concise enough to put on a
  results slide.
- **EVAL-OUT-3** — THE SYSTEM SHALL list the cases it got wrong, each with the
  identifier, the expected cause, the actual cause, the similarity score, and
  the failure category.
- **EVAL-OUT-4** — THE SYSTEM SHALL provide a breakdown by detector kind (and
  separately, cases with no explanation), so a weak detector is visible.
- **EVAL-OUT-5** — THE SYSTEM SHALL also emit a machine-readable (JSON) report
  containing the same information, including per-case records.
- **EVAL-OUT-6** — THE SYSTEM SHALL exit 0 when the pass rate meets or exceeds
  a configurable minimum, and non-zero otherwise, so it is usable as a CI gate.
- **EVAL-OUT-7** — WHEN a minimum pass rate is not configured, THE SYSTEM SHALL
  still report the pass rate and SHALL choose a documented default verdict
  behavior.
- **EVAL-OUT-8** — THE SYSTEM SHALL report unmatched entries (both directions)
  in both output formats.

## 4. Non-functional requirements

- **EVAL-NFR-1** — Evaluation SHALL be reproducible offline on a captured
  results file, so a run can be re-scored without the model.
- **EVAL-NFR-2** — Evaluation SHALL scale to thousands of cases without
  meaningful memory growth (streaming parse of the results file).
- **EVAL-NFR-3** — The ground-truth contract SHALL be documented well enough
  that the separate evaluation/test-harness repository can be written against
  it without reading Watcher's source.

## 5. Traceability

Mechanism rationale — the identifier modes, the similarity function, and the
report shapes — is in [`design.md`](./design.md); task breakdown is in
[`tasks.md`](./tasks.md).
