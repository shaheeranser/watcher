package context

import (
	"fmt"
	"strings"

	"github.com/shaheeranser/watcher/internal/detect"
	"github.com/shaheeranser/watcher/internal/source"
)

// Curation is the model input together with the stack it was built from. Block
// is the reassembled stack, which is what gets fingerprinted, so trimming the
// excerpt for the budget can never change a crash's identity.
type Curation struct {
	Excerpt string
	Block   []source.Line
}

// Curator assembles the bounded window around an event: up to a fixed number
// of preceding lines from the ring, the reassembled block, and the detector's
// tail.
type Curator struct {
	before int
	budget int
	ring   *Ring
}

func New(before, budget int, ring *Ring) *Curator {
	return &Curator{before: before, budget: budget, ring: ring}
}

func (c *Curator) Build(event detect.Event) Curation {
	var before []source.Line
	if c.ring != nil {
		before = c.ring.Preceding(event.Trigger, c.before)
	}
	block, after := reassemble(event.Kind, event.Block, event.Tail)

	header := headerFor(event)
	before, trimmedBlock, after := fit(header, before, block, after, c.budget)

	return Curation{
		Excerpt: render(header, before, trimmedBlock, after),
		Block:   block,
	}
}

func headerFor(event detect.Event) string {
	source := event.Trigger.Source
	if source == "" && len(event.Block) > 0 {
		source = event.Block[0].Source
	}
	return fmt.Sprintf("source: %s\ndetector: %s\n\n", source, event.Kind)
}

// render writes every line verbatim. The excerpt is never normalized, because
// the model's evidence is checked against this exact text (CORE-CTX-4).
func render(header string, groups ...[]source.Line) string {
	var b strings.Builder
	b.WriteString(header)
	for _, group := range groups {
		for _, line := range group {
			b.WriteString(line.Raw)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
