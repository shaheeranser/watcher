# Style Guide

Project-wide, applying to every milestone. Milestone-specific rationale lives in
`specs/<milestone>/design.md`; this file covers how code should *read*, not what
it should do.

Examples are Go, but the rules are about code shape and apply to any language in
the repository.

## 1. One concern per file

Each file holds one type, one interface implementation, or one clearly bounded
piece of logic. If you cannot name a file for what it does without reaching for
"utils", "helpers", or "common", it is holding more than one concern.

**Bad** — a grab-bag whose only shared trait is that the functions are small:

```go
// internal/utils/utils.go
package utils

func ParseTimestamp(s string) (time.Time, error) { ... }
func HashBlock(b []byte) string                  { ... }
func RenderSeverity(s Severity) string           { ... }
func SendWebhook(url string, p Payload) error    { ... }
```

**Good** — one file per concern, named for the concern:

```go
internal/fingerprint/hash.go        // HashBlock: one bounded piece of logic
internal/sink/webhook/client.go     // the webhook Sink implementation
internal/detect/block.go            // crash block assembly
internal/sink/terminal/render.go    // severity rendering
```

A type and the methods that belong to it are still one concern. The test is
whether a reader can describe the file's job in one sentence without saying
"and".

## 2. Comments describe behavior and intent, never mechanics

A comment exists to say what the code accomplishes for the system and why —
especially when the reason is not obvious. It must not restate operations the
reader can already see.

**Bad** — narrating the code back to the reader:

```go
// Loop over the block lines and append each one to the slice.
for _, line := range block {
    out = append(out, line)
}
```

**Good** — explaining why the loop exists, which the code cannot convey:

```go
// A new goroutine header starts a different crash, so stopping here keeps two
// unrelated panics from collapsing into one fingerprint.
for _, line := range block {
    out = append(out, line)
}
```

## 3. Prefer self-explanatory code over comments

If a better name or a clearer structure can carry the intent, do that instead of
writing a comment. A comment that exists only to explain a name is a signal to
rename.

**Bad** — the comment does work the code should do:

```go
// Returns true if the fingerprint has not been explained within the window.
func check(fp Fingerprint) bool { ... }
```

**Good** — the name carries it, so no comment is needed:

```go
func needsFreshExplanation(fp Fingerprint, now time.Time) bool { ... }
```

The same applies to structure:

```go
// Bad: the reader has to reconstruct the rule.
if now.Sub(inc.lastNotified) < throttle {
    return
}

// Good: the rule has a name.
if !inc.dueForNotification(now, throttle) {
    return
}
```

## 4. No comment blocks or decorative banners

Inline, one-line comments only, and only where they earn their place. Section
banners, ASCII rules, and multi-paragraph headers add noise that ages badly and
makes diffs harder to read.

**Bad**:

```go
// ============================================================
// --- Fingerprinting -----------------------------------------
// ============================================================
//
// The fingerprint is produced in three steps:
//   1. normalize volatile tokens
//   2. hash the normalized text
//   3. truncate to 16 hex characters
//
// Changing the order of these steps changes every fingerprint, so do
// not reorder them.

func Of(kind string, block []Line) Fingerprint { ... }
```

**Good**:

```go
// Order matters: normalizing after hashing would make every fingerprint depend
// on volatile tokens.
func Of(kind string, block []Line) Fingerprint { ... }
```

Package documentation is the one place a block comment belongs. It goes in
`doc.go` or directly above `package`, and describes the package's job, not its
internals.

## 5. Tests are part of the design

Tests are written with the code, not bolted on afterwards, and these conventions
are what keep them fast, deterministic, and readable.

- **Table-driven by default.** A slice of cases, each with a `name` and its
  inputs and expectations, run through `t.Run`. A single obvious case gets a
  small direct test, not a table.
- **Golden files for anything that parses or renders text** — log blocks,
  fingerprints, JSON, terminal output. They live in the package's `testdata/`
  and are regenerated with an `-update` flag, e.g.
  `go test ./internal/sink/ -run Golden -update`, so a golden diff is a
  reviewable artifact rather than a mystery.
- **Property-based tests for pure logic with invariants** — normalization,
  hashing, framing. Use `testing/quick` with a custom `Generator`; do not
  hand-pick a few inputs and call it property-based.
- **Tests run offline and deterministically.** No live Ollama, Docker, or
  network: stand in with `httptest` and fakes, and fix timestamps so goldens do
  not churn. A test that genuinely needs a service is an integration test and
  must be clearly separated and skippable.
- **Test files sit beside the code**, named `<file>_test.go`, in the same
  package unless they exercise only the exported API.

## 6. Inject what you depend on

A constructor takes what it needs — an `io.Reader` or `io.Writer`, a
`*slog.Logger`, a clock, an HTTP client — so a test can pass a fake and capture
output. Reaching for `os.Stdin`, `os.Stdout`, or `time.Now` inside logic makes
behavior untestable; take them as parameters instead.

```go
// Good: the writer is a seam a test can fill.
func NewTerminal(w io.Writer, color bool) *Terminal { ... }

// Bad: the destination is welded in and cannot be captured.
func NewTerminal(color bool) *Terminal { return &Terminal{w: os.Stdout} }
```

## 7. Errors and validation

- Wrap with `%w` and enough context to locate the failure; never discard the
  original error.
- When several independent things can be wrong — a configuration, a set of
  entries — collect them with `errors.Join` and report all of them at once,
  rather than failing on the first and making the caller iterate.
- No `panic` outside `main` and init-time programming errors. A malformed log
  line, a missing file, or a failed request is an error to handle, not a crash.

## 8. Naming and imports

- A type is unexported unless a sibling package genuinely needs it.
- When a package name collides with the standard library, alias the import at
  the call site rather than renaming the package. `internal/context` keeps the
  name its path implies; its one caller writes
  `cctx "…/internal/context"`.
- Constructors are `New` for the package's main type and `NewThing` for a
  specific implementation. An interface implementation identifies itself with
  `Name() string`.

## 9. Dependencies

The standard library comes first; add a dependency only when it does something
the standard library genuinely cannot, and record the reason. The one dependency
in this project is `golang.org/x/term`: `os.FileMode&os.ModeCharDevice` cannot
tell a real terminal from `/dev/null`, and getting that wrong silently changes
the output format.

## 10. Concurrency

- Bound every queue and channel, so a slow stage applies backpressure instead
  of growing memory without limit.
- Guard shared maps with a mutex; use atomics for counters that are only
  incremented.
- Every goroutine has an owner that waits for it. Shutdown cancels a context and
  waits on a `sync.WaitGroup`, and tests assert that no goroutines leak.
