package sink

import (
	"io"
	"os"

	"golang.org/x/term"
)

// New selects the sink for a stream. A terminal gets readable text; anything
// else gets JSON Lines so a pipe or redirect is machine-readable (CORE-OUT-1,
// CORE-OUT-2).
func New(w io.Writer, isTTY bool) Sink {
	if isTTY {
		return NewTerminal(w, os.Getenv("NO_COLOR") == "")
	}
	return NewJSONL(w)
}

// IsTerminal reports whether f is attached to a terminal. It lives here so the
// terminal dependency stays inside the sink package.
func IsTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}
