// Package fingerprint identifies "the same crash happening again". It rewrites
// the volatile parts of a detected block to stable placeholders and hashes the
// result, so occurrences that differ only in addresses, ids, or timestamps
// collapse to one identity while different root causes stay apart.
package fingerprint

import "github.com/shaheeranser/watcher/internal/source"

// Fingerprint is a crash identity. Normalized is retained for debugging and
// for tests that need to see why two blocks did or did not match.
type Fingerprint struct {
	Hash       string
	Normalized string
	Kind       string
}

func Of(kind string, block []source.Line) Fingerprint {
	normalized := Normalize(kind, block)
	return Fingerprint{
		Hash:       hashNormalized(kind, normalized),
		Normalized: normalized,
		Kind:       kind,
	}
}
