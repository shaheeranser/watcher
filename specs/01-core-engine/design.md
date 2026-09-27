# 01 — Core Engine: Design

Requirements: [`requirements.md`](./requirements.md). Tasks:
[`tasks.md`](./tasks.md).

Dependencies: this milestone depends only on milestone 00 (repository
scaffolding). It defines the interfaces later milestones extend.

## 1. Goals and constraints

The design is shaped by one hard constraint: **the model is small** (a
few-hundred-million to low-billion parameter model served by Ollama). That
means:

- Context curation is a first-class component, not a formatting detail.
  A small model degrades quickly with noisy, oversized input.
- Output must be constrained to a small schema, because a small model will not
  reliably honor a prose format.
- Model calls are expensive relative to line parsing, so fingerprinting must
  suppress repeat calls (CORE-FP-6).

A second constraint is **push, not pull**: the process runs unattended, so the
pipeline must never block on a human and must degrade gracefully when the model
or a sink misbehaves.

## 2. Pipeline overview

```
 stdin / file ──▶ ┌──────────┐
                  │  Source  │ ──▶ Line ──┐
                  └──────────┘            │
                                          ▼
                                 ┌────────────────┐
                                 │    Detector    │ ──▶ Event ──┐
                                 └────────────────┘             │
                                                                ▼
                                                  ┌──────────────────────────┐
                                                  │ Context curation         │
                                                  │ (bounded window/excerpt) │
                                                  └────────────┬─────────────┘
                                                               │ excerpt
                        ┌──────────────────────────────────────┤
                        ▼                                      ▼
              ┌──────────────────┐                  ┌────────────────────┐
              │  Fingerprinter   │ ── Fingerprint ─▶│  Incident tracker  │
              └──────────────────┘                  │  (counts, times)   │
                                                    └─────────┬──────────┘
                                                              │ on first sight / window elapsed
                                                    ┌─────────▼──────────┐
                                                    │ Backend (Ollama)   │
                                                    └─────────┬──────────┘
                                                              │ Explanation
                                                    ┌─────────▼──────────┐
                                                    │ Sink (TTY / JSONL) │
                                                    └────────────────────┘
```

The `Source → Detector → Backend → Sink` chain is the spine. Fingerprinting,
context curation, and incident tracking sit between `Detector` and `Backend`,
and exist so that the expensive stage (the model) is called rarely and with
good input.

## 3. Package layout

```
cmd/watcher/            main: flag/env parsing, wiring, signal handling
internal/config/        config struct, defaults, validation, precedence
internal/source/        Source interface; stdin.go; file.go
internal/detect/        Detector; patterns.go; block.go (assembly)
internal/fingerprint/   normalize.go; hash.go
internal/context/       window.go; reassemble.go; budget.go
internal/incident/      tracker.go; incident.go (counts/state, process-local)
internal/backend/       Backend interface; ollama.go; prompt.go; schema.go
internal/sink/          Sink interface; terminal.go; jsonl.go
internal/guard/         self.go (own-output detection)
internal/engine/        pipeline wiring: goroutines, channels, shutdown
```

`internal/*` keeps the interface boundaries private to the module while
letting tests in sibling packages use them. `cmd/watcher` is the only place
that knows about all four interfaces at once.

## 4. Core interfaces

```go
// internal/source
type Line struct {
    Raw       string    // verbatim text, no trailing newline
    Source    string    // "stdin", "/var/log/app.log", ...
    ArrivedAt time.Time
}

type Source interface {
    // Stream sends Lines until ctx is cancelled or the source ends, then
    // closes the channel. It returns a non-nil error only for setup failures;
    // mid-stream read errors are surfaced on the channel's companion error
    // field or by closing early with a recorded reason.
    Stream(ctx context.Context) (<-chan Line, error)
    Name() string
}
```

```go
// internal/detect
type Event struct {
    Kind      string    // "go-panic" | "python-traceback" | "generic-fatal"
    Trigger   Line      // the line that matched
    Block     []Line    // the assembled multi-line block, verbatim
    Tail      []Line    // up to M lines following the block
    DetectedAt time.Time
}

type Detector interface {
    // Detect consumes Lines and emits Events. Lines that match no pattern
    // produce no Event.
    Detect(ctx context.Context, in <-chan Line, out chan<- Event) error
    Name() string
}
```

```go
// internal/fingerprint
type Fingerprint struct {
    Hash       string // hex, truncated to 16 chars
    Normalized string // useful for debugging and tests
    Kind       string
}

func Normalize(kind string, block []Line) string
func Of(kind string, block []Line) Fingerprint
```

```go
// internal/backend
type Explanation struct {
    Summary      string   `json:"summary"`
    LikelyCause  string   `json:"likely_cause"`
    Evidence     []string `json:"evidence"`
    SuggestedFix string   `json:"suggested_fix"`
    Confidence   float64  `json:"confidence"`
    Severity     string   `json:"severity"`
}

type Request struct {
    Kind     string
    Excerpt  string   // verbatim, already budget-capped
    Fingerprint string
}

type Backend interface {
    Explain(ctx context.Context, req Request) (Explanation, error)
    Name() string
}
```

```go
// internal/sink
type Result struct {
    Fingerprint string
    Kind        string
    Source      string
    Count       int
    FirstSeen   time.Time
    LastSeen    time.Time
    Explanation *backend.Explanation // nil => explanation unavailable
    ExplainErr  string               // populated when Explanation is nil
    Model       string
}

type Sink interface {
    Emit(ctx context.Context, r Result) error
    Name() string
}
```

The `Sink` interface is intentionally minimal in this milestone: it emits a
finished `Result`. Later milestones add lifecycle kinds (new / still-happening
/ resolved) by extending `Result` with a kind field and adding sinks — they do
not change this contract's shape.

## 5. Detector design

### 5.1 Patterns

Patterns are compiled once at startup. The trigger regexes:

| Kind | Trigger | Block assembly |
|------|---------|----------------|
| `go-panic` | `^panic:` or `^fatal error:` | include the following `goroutine \d+ \[.*\]:` header and contiguous frames matching `^\t.*\.go:\d+(\s+\+0x[0-9a-f]+)?$`; also capture a trailing `exit status \d+` |
| `python-traceback` | `^Traceback \(most recent call last\):` | include contiguous lines until (and including) the first `^[A-Za-z_][\w.]*(Error|Exception|Warning)?:` line that is not indented |
| `generic-fatal` | `\b(FATAL|CRITICAL)\b` or `^(Unhandled )?\w*(Exception|Error):` | include following indented continuation lines; stop at the first line that starts a new log record (timestamp/level prefix) |

### 5.2 Block assembly

Assembly is a small state machine per pattern:

1. **Idle** — scan lines for a trigger.
2. **Collecting** — accept lines while the pattern's continuation predicate
   holds; the predicate is a per-kind function (indentation, frame shape, or
   "not a new record").
3. **Terminate** — on terminator (blank line, new log record, or frame count
   exceeded), emit the `Event` and return to Idle.

The collector also retains up to M following lines as `Event.Tail`, which the
context curator uses (CORE-CTX-1). The collector caps block size (CORE-DET-6)
by keeping the head and tail of the block with an elision marker line so the
elision is visible in the excerpt.

### 5.3 Why heuristics, not a parser

There is no universal log format, and requiring one would break the
deployment-agnostic goal. Patterns are deliberately broad and are expected to
be extended. Every pattern is testable in isolation against golden fixtures.

## 6. Fingerprint normalization

Normalization is a deterministic, ordered rewrite of the block text, followed
by a hash. Order matters (e.g. timestamps must be rewritten before generic
number collapsing, if any). Rules, in order:

| # | Target | Regex (illustrative) | Placeholder |
|---|--------|----------------------|-------------|
| 1 | Hex addresses / offsets | `0x[0-9a-fA-F]+` | `0xADDR` |
| 2 | UUIDs | `\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b` | `<UUID>` |
| 3 | Timestamps | RFC3339, `2006-01-02 15:04:05(.\d+)?`, syslog `[A-Z][a-z]{2} +\d+ \d\d:\d\d:\d\d` | `<TS>` |
| 4 | Request/trace IDs | `\b(request[_-]?id|req[_-]?id|trace[_-]?id|span[_-]?id|x-request-id)=\S+` (case-insensitive) | `<ID>` |
| 5 | Goroutine IDs | `goroutine \d+` | `goroutine N` |
| 6 | Thread/process IDs | `\b(pid|tid)[=: ]\s*\d+`, `\[\d+\]` bracket PIDs | `<PID>` |
| 7 | Durations | `\d+(\.\d+)?\s?(ns|µs|us|ms|s)\b` | `<DUR>` |
| 8 | Temp paths | `/tmp/[\w.\-]+`, `/var/folders/[\w/.\-]+` | `<TMP>` |
| 9 | Ports | `:\d{4,5}\b` | `:PORT` |
| 10 | Memory sizes | `\d+(\.\d+)?\s?(B|KB|MB|GB)\b` | `<SIZE>` |

After rewriting: strip trailing whitespace per line, collapse runs of
whitespace to a single space, drop empty lines, and join with `\n`. Then
`hash = sha256(kind + "\n" + normalized)`, hex-encoded, truncated to 16 chars.

Invariants the property tests assert (CORE-FP-7):

1. **Idempotence** — `Normalize(Normalize(x)) == Normalize(x)`.
2. **Volatile invariance** — substituting any volatile token for another of
   the same class does not change the fingerprint.
3. **Distinctness** — differing exception types or differing normalized frame
   sets yield different fingerprints.
4. **Determinism** — same input always yields the same hash.

## 7. Context curation

Given an `Event`, the curator produces a verbatim excerpt string:

1. Take up to N preceding lines (from a bounded ring buffer of recent lines,
   CORE-NFR-3), the block, and up to M following lines.
2. **Reassemble**: if the source interleaved unrelated lines inside a
   traceback, drop the interleaved lines that fail the pattern's continuation
   predicate, and mark the elision with `…`. This prevents the model from
   reasoning over a corrupted stack (CORE-CTX-2).
3. **Budget**: if the excerpt exceeds the configured byte budget
   (CORE-CTX-3), trim in the order: middle stack frames first, then preceding
   context, then following context — never the trigger line, the root
   exception line, or the top three frames.
4. Add a short header describing the source and detector kind so the model
   knows what it is reading.

The excerpt is never normalized — evidence must be quotable verbatim
(CORE-CTX-4).

## 8. Backend: Ollama integration

- Endpoint: `POST {base_url}/api/chat`, `stream: false`,
  `options.temperature: 0` for reproducibility.
- Structured output: use Ollama's `format` field with a JSON schema derived
  from `Explanation`. If the installed Ollama build does not support schema
  constraining, fall back to `format: "json"` plus strict client-side
  validation (`schema.go`) — the client-side validator is required either way.
- Validation failures (CORE-BE-4) trigger up to K retries with the same prompt;
  persistent failure yields a `Result` with a nil `Explanation` and
  `ExplainErr` populated. The pipeline treats that as a reportable outcome, not
  an error.
- Evidence grounding (CORE-BE-5): each returned evidence string is checked with
  `strings.Contains(excerpt, evidence)`. Non-matching entries are dropped and
  `Confidence` is floored to 0.3 with a note appended to `Summary`.
- Concurrency: a worker pool of size W (default 1, CORE-BE-7) consumes
  explanation jobs; a per-request timeout (CORE-BE-8) bounds each call.

### 8.1 Prompt template

```
You are a reliability engineer's assistant. Explain the root cause of the
crash in the log excerpt below.

Respond with ONLY a JSON object matching this schema:
{"summary": string,
 "likely_cause": string,
 "evidence": string[],
 "suggested_fix": string,
 "confidence": number,   // 0.0-1.0
 "severity": "low"|"medium"|"high"|"critical"}

Rules:
- Every string in "evidence" MUST be copied verbatim from the excerpt.
- If the excerpt is insufficient, say so in "summary" and lower "confidence".
  Do not invent details.

Detector kind: {{kind}}
Source: {{source}}

<excerpt>
{{excerpt}}
</excerpt>
```

## 9. Guardrail (never watch yourself)

`internal/guard` exposes `IsSelf(src Source) (bool, string)`. Checks, in order:

1. **File identity** — if the source is a file, compare its device+inode
   against `os.Stdout` and `os.Stderr` (via `os.Stat`/`Lstat` and, on Linux,
   `/proc/self/fd/1` and `/proc/self/fd/2`). A match means we are tailing our
   own redirected output.
2. **Writer PID** — on Linux, resolve the openers of the file via
   `/proc/*/fd` and refuse if the only writer is our own PID. (Best-effort;
   the file-identity check is authoritative when it applies.)
3. **Container identity** — the hook is defined here so that any source which
   can report a container ID is checked against our own ID. It is a no-op for
   sources that cannot report one.

A positive result is fatal: stderr diagnostic, exit non-zero (CORE-GRD-2).
This guard is what makes `watcher` safe to leave running unattended.

## 10. Output renderers

Two `Sink` implementations, selected once at startup by
`term.IsTerminal(int(os.Stdout.Fd()))`:

- **`terminal`** — colored (disabled when `NO_COLOR` is set) block per
  incident: severity header, fingerprint + count + first/last seen, summary,
  likely cause, evidence as a quoted list, suggested fix, confidence.
- **`jsonl`** — one JSON object per line, field names exactly as in
  CORE-OUT-3, timestamps in RFC3339 UTC. Never pretty-printed; one compact
  line per incident.

Both take `io.Writer` in their constructors so tests can capture output.
Diagnostics never touch stdout (CORE-GRD-3, CORE-OUT-4).

## 11. Concurrency and shutdown

- One goroutine per stage (`source`, `detect`, `curate+fingerprint`,
  backend pool), connected by buffered channels (default buffer 1024 lines).
- The source→detector path never blocks on the backend: detected events go to
  the tracker, which decides whether a model call is warranted (CORE-NFR-4,
  CORE-FP-6). If the model is slow, the tracker's counts keep rising; the
  explanation is attached when it arrives.
- `context.Context` cancellation propagates from `main` on signal; every stage
  selects on `ctx.Done()`. Shutdown waits on a `sync.WaitGroup` with a bounded
  grace period, then returns (CORE-NFR-1).

## 12. Configuration

Precedence: **flag > environment variable > default**. A single `config.Config`
struct is populated by `internal/config` and validated at startup.

| Setting | Flag | Env | Default |
|---------|------|-----|---------|
| Source file | `--file` | `WATCHER_FILE` | (stdin) |
| Ollama base URL | `--ollama-url` | `WATCHER_OLLAMA_URL` | `http://localhost:11434` |
| Model | `--model` | `WATCHER_MODEL` | *(required, no default — see §15)* |
| Preceding context lines (N) | `--context-before` | `WATCHER_CONTEXT_BEFORE` | `20` |
| Following context lines (M) | `--context-after` | `WATCHER_CONTEXT_AFTER` | `10` |
| Excerpt byte budget | `--context-budget` | `WATCHER_CONTEXT_BUDGET` | `8192` |
| Backend workers | `--workers` | `WATCHER_WORKERS` | `1` |
| Request timeout | `--ollama-timeout` | `WATCHER_OLLAMA_TIMEOUT` | `60s` |
| Explanation window | `--explain-window` | `WATCHER_EXPLAIN_WINDOW` | `15m` |
| Max block lines | `--max-block-lines` | `WATCHER_MAX_BLOCK_LINES` | `200` |
| Start file from beginning | `--from-start` | `WATCHER_FROM_START` | `false` |

## 13. Failure modes

| Failure | Behavior |
|---------|----------|
| Source file disappears | Retry with backoff; keep the process alive; log to stderr |
| Source file rotated | Reopen at new path/inode, continue |
| Ollama unreachable | Bounded retry/backoff; emit `explanation unavailable` result |
| Model returns invalid JSON | Retry up to K; else emit unavailable result |
| Evidence not grounded | Drop offending entries; floor confidence |
| Own output detected | Fatal, stderr diagnostic, non-zero exit |
| Slow sink | Bounded queue; documented drop policy with a dropped counter logged to stderr |

## 14. Testing strategy

- **Detector**: table-driven tests over golden fixture logs (checked-in
  `testdata/*.log` + expected `Event` blocks).
- **Fingerprint**: property-based tests for the four invariants in §6, plus
  example tests for each rewrite rule.
- **Context curation**: tests for interleaving reassembly, budget trimming
  order, and verbatim preservation.
- **Backend**: `httptest` server standing in for Ollama; tests for valid JSON,
  malformed JSON, ungrounded evidence, timeouts, and 500s.
- **Guardrail**: tests using a temp file pointed at by both a `Source` and a
  fake stdout FD.
- **Output**: golden-file tests for both renderers.
- **End-to-end**: feed a fixture log through stdin with a fake backend; assert
  JSONL output and that repeat occurrences increment `count` without a second
  model call.

## 15. Open decisions

These were open when the milestone was drafted. They are now resolved, and the
values below are what the implementation uses; the reasoning is recorded so a
later milestone can revisit a choice rather than rediscover it.

- **OD-01-1 — Default model.** *Resolved:* there is no compiled-in default.
  `--model` / `WATCHER_MODEL` is required and startup validation fails without
  it. A model name is a property of the deployment — the sibling Ollama
  container, the systemd unit, the command line — not of the binary. Watcher
  never installs or pulls a model, so baking in a vendor-specific tag would add
  a hardware assumption without saving the operator any work. This also keeps
  the `Backend` interface swappable, which is the point.
- **OD-01-2 — Context window.** *Resolved:* N = 20 preceding, M = 10 following.
  Enough lead-in to catch a causal line above the crash without flooding a small
  model.
- **OD-01-3 — Excerpt budget.** *Resolved:* 8192 bytes. Comfortably inside a
  small model's context while keeping a full stack trace.
- **OD-01-4 — Explanation window.** *Resolved:* 15 minutes. One model call per
  fingerprint per window; repeats in between only raise the count.
- **OD-01-5 — Case folding in normalization.** *Resolved:* normalization does
  **not** lowercase. Stack frames and identifiers stay exact; the small risk of
  near-duplicate fingerprints is preferred to collapsing genuinely distinct
  casing.
- **OD-01-6 — File start position.** *Resolved:* a file source starts at the
  current end; `--from-start` opts into reading existing content.
- **OD-01-7 — Fingerprint fixture corpus.** *Resolved:* property tests generate
  volatile-token variants in memory. No corpus is checked in, so there is
  nothing to drift from the normalization rules.

One deviation from §4 worth recording: `backend.Request` gained a `Source`
field, because the prompt template in §8.1 interpolates `{{source}}` and the
request shape given in §4 had no way to supply it.
