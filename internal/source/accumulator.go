package source

import (
	"bytes"
	"io"
	"strings"
)

// lineAccumulator frames a byte stream into lines. It only ever splits on a
// newline, so a line is delivered whole no matter how long it is (CORE-SRC-6),
// and an unterminated tail stays buffered until the caller chooses to flush it.
type lineAccumulator struct {
	r       io.Reader
	buf     []byte
	pending []byte
}

func newLineAccumulator(r io.Reader) *lineAccumulator {
	return &lineAccumulator{r: r, buf: make([]byte, 32*1024)}
}

// read performs one read and returns every complete line it now holds. The
// read error is returned alongside those lines so a caller can act on both the
// final data and the reason the stream stopped.
func (a *lineAccumulator) read() ([]string, error) {
	n, err := a.r.Read(a.buf)
	if n > 0 {
		a.pending = append(a.pending, a.buf[:n]...)
	}

	var lines []string
	for {
		i := bytes.IndexByte(a.pending, '\n')
		if i < 0 {
			break
		}
		lines = append(lines, strings.TrimSuffix(string(a.pending[:i]), "\r"))
		a.pending = a.pending[i+1:]
	}
	if len(a.pending) == 0 {
		a.pending = nil
	}
	return lines, err
}

// takePending returns the unterminated remainder and clears it. Callers use it
// at end of stream, where no more bytes can complete the line.
func (a *lineAccumulator) takePending() (string, bool) {
	if len(a.pending) == 0 {
		return "", false
	}
	line := strings.TrimSuffix(string(a.pending), "\r")
	a.pending = nil
	return line, true
}
