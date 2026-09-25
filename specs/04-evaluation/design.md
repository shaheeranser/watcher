# 04 — Evaluation: Design

Requirements: [`requirements.md`](./requirements.md). Tasks:
[`tasks.md`](./tasks.md).

Dependencies: builds on `../01-core-engine/design.md` (the `Result` type and
the JSONL output contract) and consults `../03-dashboard/design.md` only to
confirm that evaluation does **not** need the database or the API. It does not
depend on the Docker source or the state machine, which keeps scoring usable
from a plain file.

## 1. Shape of the feature

`watcher eval` is a pure, offline, second entry point:

```
  truth file (JSON|CSV)  ─┐
                          ├──▶  watcher eval  ──▶  text tally (slide)
  results (JSONL|stdin)  ─┘                    └──▶  JSON report (CI)
                                                   exit code (CI gate)
```

It performs no detection, no fingerprinting, and no model calls. Everything it
needs already exists in the two input files, which is what makes a run
re-scorable later without the model (`EVAL-NFR-1`) and keeps the harness
decoupled (`EVAL-NFR-3`).

## 2. The identifier problem

Scoring requires correlating "this ground-truth entry" with "this Watcher
output". Two identifiers are supported, because the right one depends on how
the harness is structured.

### 2.1 Fingerprint mode (`--identifier fingerprint`)

Watcher already emits a stable `fingerprint` per incident
(`../01-core-engine/design.md` §6). A harness that captures a single known
crash can key ground truth on that hash:

```json
{"a1b2c3d4e5f6a7b8": "nil pointer dereference in config loader"}
```

Pros: no changes needed, and the key is inherently stable across runs because
normalization strips volatile tokens. Cons: the harness must first learn the
fingerprint, which is circular for a single run (it must run Watcher once, read
the hash, then write truth). Good for regression suites with a fixed corpus.

### 2.2 Run-id mode (`--identifier run_id`)

The harness runs one scenario at a time and tells Watcher which run it is:

```
WATCHER_RUN_ID=run-014  watcher --file scenario-014.log --json > results.jsonl
```

This milestone adds an additive, optional field to the result contract:

```json
{"type": "incident", "run_id": "run-014", "fingerprint": "…", "…": "…"}
```

Every emitted incident inherits the run id, so a harness that already knows its
own scenario identifiers can key ground truth directly on them:

```json
{"run-014": "nil pointer dereference in config loader"}
```

Pros: no circularity; the harness's own identifiers flow through untouched.
Cons: requires the harness to run scenarios one at a time, or to set a distinct
run id per scenario. This is the recommended mode, and it is the reason the
contract is "one run id in, echoed in every result" rather than "Watcher
invents an id".

### 2.3 Why both

Fingerprint mode supports "score the whole corpus by crash identity"; run-id
mode supports "score each scenario against its expected diagnosis". Both read
the same plain files. Neither requires the harness to know anything about
Watcher beyond the documented JSONL fields.

## 3. Input formats

### 3.1 Ground truth

**JSON** — either a bare object (id → expected cause) or a `cases` array with
optional extra fields:

```json
{
  "cases": [
    {"id": "run-001", "expected_cause": "nil pointer dereference in config loader", "kind": "go-panic"},
    {"id": "run-002", "expected_cause": "unhandled KeyError: 'user_id' in worker", "kind": "python-traceback"}
  ]
}
```

```json
{
  "run-001": "nil pointer dereference in config loader",
  "run-002": "unhandled KeyError: 'user_id' in worker"
}
```

**CSV** — a header row with `id` and `expected_cause` (and optional `kind`):

```csv
id,expected_cause,kind
run-001,"nil pointer dereference in config loader",go-panic
run-002,"unhandled KeyError: 'user_id' in worker",python-traceback
```

Rules: `id` and `expected_cause` are required and non-empty; duplicate `id`s
are an error (`EVAL-GT-4`); unknown columns are ignored so the format can grow
without breaking older readers.

### 3.2 Actual results

The JSONL incident stream from milestone 01. Parsing is streaming and
line-by-line (`EVAL-NFR-2`): blank lines are skipped, unparseable lines are
counted and reported (not fatal), and unknown fields are ignored
(`EVAL-ACT-3`).

## 4. Similarity and scoring

### 4.1 Normalization

Both expected and actual causes are normalized before comparison
(`EVAL-SCORE-2`):

1. Unicode NFKC; lowercase.
2. Strip punctuation and collapse whitespace.
3. Drop a small stopword set (`a`, `the`, `in`, `of`, `on`, `at`, `to`, `is`,
   `was`).

### 4.2 Similarity

Free-text causes cannot be compared exactly — two humans will not phrase the
same root cause identically, and neither will a small model. The score is the
maximum of two complementary measures:

- **Token-set similarity** — Sørensen–Dice over normalized token sets,
  tolerant of word order and of one side using more words.
- **Character 3-gram similarity** — Dice over character trigrams, tolerant of
  morphological differences (`loading` / `loads` / `failed to load`).

A case **passes** when:

- the normalized strings are exactly equal (always a pass), **or**
- the score is `>= τ`, where `τ` is the configurable threshold
  (`EVAL-SCORE-3`).

The raw score is recorded per case regardless of the threshold, so a stored run
can be re-thresholded without re-running anything (`EVAL-SCORE-4`) — this is
what makes the threshold safe to tune after the fact.

### 4.3 Failure categories

Each case is classified into exactly one category:

| Category | Meaning |
|----------|---------|
| `pass` | Score met the threshold (or exact match) |
| `cause_mismatch` | Both sides present; score below threshold |
| `no_explanation` | Result exists but carries no explanation (CORE-BE-4 outcome) |
| `no_result` | Ground-truth entry with no corresponding result (`EVAL-MATCH-3`) |
| `kind_mismatch` | Cause passed but the expected detector kind differs (recorded, fails only under `--require-kind`) |

Kind mismatch is reported separately from cause mismatch (`EVAL-SCORE-6`)
because "Watcher understood the crash but the detector labeled it differently"
is a different bug from "Watcher misdiagnosed the cause".

### 4.4 Multiple results for one identifier

In run-id mode a scenario can legitimately produce more than one incident. The
scored result is chosen deterministically (`EVAL-MATCH-5`): highest severity,
then highest confidence, then earliest `first_seen`. The remaining results for
that identifier are reported as extra. The exact tie-break is OD-04-4.

## 5. Output

### 5.1 Text tally (slide-ready)

```
Watcher evaluation — 20 cases   (identifier: run_id, τ = 0.60)

  PASS 17 / 20   85.0%        gate: >= 60.0%  →  PASS

  by detector kind
    go-panic           6/7    85.7%
    python-traceback   5/5   100.0%
    generic-fatal      6/8    75.0%
    no explanation     0/1     0.0%

  Failures (3)
    run-014  go-panic        sim 0.21  cause_mismatch
      expected: nil pointer dereference on config load
      actual:   database connection refused
    run-019  generic-fatal   sim 0.00  no_explanation
      expected: upstream timeout from billing API
      actual:   (explanation unavailable)
    run-020  generic-fatal   sim 0.44  cause_mismatch
      expected: disk full writing WAL
      actual:   permission denied on /var/lib/app

  Unmatched
    truth only (no result produced): run-007
    results only (no truth entry):   a1b2c3d4e5f6a7b8
```

### 5.2 JSON report (CI)

```json
{
  "schema_version": 1,
  "identifier": "run_id",
  "threshold": 0.60,
  "min_pass_rate": 0.60,
  "total": 20,
  "passed": 17,
  "failed": 3,
  "pass_rate": 0.85,
  "verdict": "pass",
  "by_kind": {
    "go-panic":         {"total": 7, "passed": 6},
    "python-traceback": {"total": 5, "passed": 5},
    "generic-fatal":    {"total": 8, "passed": 6},
    "no_explanation":   {"total": 1, "passed": 0}
  },
  "cases": [
    {"id": "run-014", "category": "cause_mismatch", "score": 0.21,
     "expected_cause": "nil pointer dereference on config load",
     "actual_cause": "database connection refused",
     "expected_kind": "go-panic", "actual_kind": "go-panic", "kind_match": true},
    {"id": "run-019", "category": "no_explanation", "score": 0.0,
     "expected_cause": "upstream timeout from billing API",
     "actual_cause": null, "explanation_error": "ollama: no response"}
  ],
  "unmatched": {"truth_only": ["run-007"], "results_only": ["a1b2c3d4e5f6a7b8"]}
}
```

Exit code is 0 when `verdict == "pass"` and non-zero otherwise
(`EVAL-OUT-6`). The behavior when `min_pass_rate` is unset is OD-04-2.

## 6. CLI

```
watcher eval \
  --truth   truth.json|truth.csv \
  --results results.jsonl|- \
  [--identifier fingerprint|run_id] \
  [--threshold 0.60] \
  [--min-pass-rate 0.60] \
  [--require-kind] \
  [--format text|json|both]
```

- `--results -` reads stdin, enabling `watcher … --json | watcher eval …`
  (`EVAL-ACT-2`).
- Format is inferred from the file extension; `--truth-format` overrides.

## 7. Integration contract for the harness repo

The separate evaluation/test-harness repository is written against only this:

1. **Produce a scenario log** — the raw application output for one scenario.
2. **Write ground truth** — an entry `{id, expected_cause, kind?}` in the JSON
   or CSV shape in §3.1.
3. **Run Watcher for the scenario** — with `WATCHER_RUN_ID=<id>` (run-id mode)
   or against the fixed corpus (fingerprint mode), capturing the JSONL stream
   to `results.jsonl`.
4. **Score** — `watcher eval --truth <file> --results results.jsonl
   [--identifier run_id]`.
5. **Publish** — the text tally for the results slide, the JSON report for
   history, and the exit code for CI.

No step requires the harness to import Watcher code, match a Watcher-internal
type, or understand the fingerprint algorithm. The only coupling is the
documented JSONL field set and the two ground-truth file shapes.

## 8. Testing strategy

- **Input parsing** — table-driven tests for JSON object, JSON `cases`, CSV,
  empty files, duplicate ids, missing required fields, unicode, quoted commas
  in CSV.
- **Matching** — tests for both identifier modes; truth-only and result-only
  cases; multiple results for one id with the deterministic tie-break.
- **Similarity** — example tests for exact match, word-order variation,
  morphological variation, and genuinely different causes; boundary tests at
  `τ`.
- **Reports** — golden-file tests for the text tally and the JSON report.
- **Exit codes** — table tests across pass rates straddling `min_pass_rate`.
- **Re-thresholding** — score a stored run at two thresholds and assert only
  the verdicts change, not the scores.
- **Robustness** — a results file with unknown fields and junk lines parses,
  reports the junk count, and still scores.

## 9. Open decisions

- **OD-04-1** — Default similarity metric and threshold `τ`. Dice-on-tokens vs.
  trigram weighting, and where the cutoff sits, materially change pass rates;
  this should be calibrated against a real corpus before being fixed.
- **OD-04-2** — Default `min_pass_rate` (or the default verdict when it is
  unset).
- **OD-04-3** — Whether `expected_cause` should also support a controlled
  vocabulary of canonical labels plus synonyms (strict label matching) as an
  alternative to free-text similarity.
- **OD-04-4** — The tie-break rule when one identifier maps to multiple results
  (severity → confidence → earliest is proposed).
- **OD-04-5** — Whether a kind mismatch should fail the case by default rather
  than only under `--require-kind`.
- **OD-04-6** — The exact env var / flag name for the run id, and whether it
  should also be accepted as a per-line field on stdin (a tagged input stream).
- **OD-04-7** — Whether to emit an additional slide-ready artifact (Markdown
  table or image) directly, or leave slide rendering to the harness.
- **OD-04-8** — Whether severity should weight the score (a missed `critical`
  counting more than a missed `low`) rather than every case counting equally.
- **OD-04-9** — Whether `watcher eval` should optionally read a run's history
  from the `../03-dashboard/design.md` database instead of a JSONL file, for
  scoring a long-lived daemon rather than a single-shot run.
