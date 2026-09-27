// Package context builds the bounded, verbatim excerpt sent to the model. It
// holds a short history of recent lines so it can attach the context preceding
// a crash, reassembles stacks the source interleaved, and trims the result to a
// byte budget without ever normalizing the text it sends.
package context

import (
	"sync"

	"github.com/shaheeranser/watcher/internal/source"
)

// Ring is a bounded history of recent lines. It is written by the goroutine
// feeding lines and read by the one building excerpts, so it is safe for
// concurrent use and keeps memory flat under sustained input (CORE-NFR-3).
type Ring struct {
	mu    sync.Mutex
	buf   []source.Line
	next  int
	count int
}

func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{buf: make([]source.Line, capacity)}
}

func (r *Ring) Add(line source.Line) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = line
	r.next = (r.next + 1) % len(r.buf)
	if r.count < len(r.buf) {
		r.count++
	}
}

// Preceding returns up to n lines that arrived immediately before trigger,
// oldest first. The trigger is located by identity, so an excerpt stays
// anchored to its exact event even as the buffer rolls forward.
func (r *Ring) Preceding(trigger source.Line, n int) []source.Line {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 || r.count == 0 {
		return nil
	}
	for i := r.count - 1; i >= 0; i-- {
		if !sameLine(r.at(i), trigger) {
			continue
		}
		start := i - n
		if start < 0 {
			start = 0
		}
		out := make([]source.Line, 0, i-start)
		for j := start; j < i; j++ {
			out = append(out, r.at(j))
		}
		return out
	}
	return nil
}

// at maps a logical index, where 0 is the oldest retained line, to the backing
// array.
func (r *Ring) at(i int) source.Line {
	idx := (r.next - r.count + i + 2*len(r.buf)) % len(r.buf)
	return r.buf[idx]
}

func sameLine(a, b source.Line) bool {
	return a.Raw == b.Raw && a.Source == b.Source && a.ArrivedAt.Equal(b.ArrivedAt)
}
