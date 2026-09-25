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
