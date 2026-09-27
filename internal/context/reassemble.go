package context

import (
	"github.com/shaheeranser/watcher/internal/detect"
	"github.com/shaheeranser/watcher/internal/source"
)

// reassemble rebuilds a coherent stack from the detected block plus the tail.
// The detector stops a block at the first line that does not belong to it, so
// a stack the source interleaved with unrelated output arrives split in two;
// here the stray lines are dropped and each gap is marked with an ellipsis so
// the model never reads a spliced-together pseudo-stack (CORE-CTX-2).
//
// It returns the cleaned block, which is what gets fingerprinted, and the
// following lines that are not part of the stack.
func reassemble(kind string, block, tail []source.Line) ([]source.Line, []source.Line) {
	if !detect.StackShaped(kind) {
		return block, tail
	}

	combined := make([]source.Line, 0, len(block)+len(tail))
	combined = append(combined, block...)
	combined = append(combined, tail...)

	// Only lines up to the last continuation can belong to the stack; anything
	// after it is ordinary following context.
	last := 0
	for i, line := range combined {
		if i == 0 || detect.IsBlockContinuation(kind, line.Raw) {
			last = i
		}
	}

	clean := make([]source.Line, 0, last+1)
	suppressed := 0
	for i := 0; i <= last; i++ {
		if i == 0 || detect.IsBlockContinuation(kind, combined[i].Raw) {
			if suppressed > 0 {
				clean = append(clean, ellipsis(combined[i-1]))
				suppressed = 0
			}
			clean = append(clean, combined[i])
			continue
		}
		suppressed++
	}
	if suppressed > 0 {
		clean = append(clean, ellipsis(combined[last]))
	}

	return clean, combined[last+1:]
}

func ellipsis(anchor source.Line) source.Line {
	return source.Line{Raw: "…", Source: anchor.Source, ArrivedAt: anchor.ArrivedAt}
}
