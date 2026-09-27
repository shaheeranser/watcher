package context

import (
	"github.com/shaheeranser/watcher/internal/source"
)

// fit reduces an over-budget excerpt by giving up the least useful material
// first: middle stack frames, then the oldest preceding context, then the
// furthest following context. The trigger, the top three frames, and the root
// exception line always survive (CORE-CTX-3).
func fit(header string, before, block, after []source.Line, budget int) ([]source.Line, []source.Line, []source.Line) {
	if excerptSize(header, before, block, after) <= budget {
		return before, block, after
	}

	block = trimMiddleFrames(block)
	for len(before) > 0 && excerptSize(header, before, block, after) > budget {
		before = before[1:]
	}
	for len(after) > 0 && excerptSize(header, before, block, after) > budget {
		after = after[:len(after)-1]
	}
	return before, block, after
}

// trimMiddleFrames keeps the head of the stack and its final line, replacing
// everything between with a marker.
func trimMiddleFrames(block []source.Line) []source.Line {
	const headKeep = 4 // the trigger plus the top three frames
	if len(block) <= headKeep+1 {
		return block
	}
	root := block[len(block)-1]
	out := make([]source.Line, 0, headKeep+2)
	out = append(out, block[:headKeep]...)
	out = append(out, ellipsis(root))
	out = append(out, root)
	return out
}

func excerptSize(header string, before, block, after []source.Line) int {
	size := len(header)
	for _, group := range [][]source.Line{before, block, after} {
		for _, line := range group {
			size += len(line.Raw) + 1
		}
	}
	return size
}
