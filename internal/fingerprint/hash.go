package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
)

// hashLength is how many hex characters of the digest identify a crash. It is
// short enough to read in output and long enough that accidental collisions
// are not a practical concern.
const hashLength = 16

// The kind is mixed into the hash so that the same text detected by different
// patterns stays distinct (CORE-FP-4).
func hashNormalized(kind, normalized string) string {
	sum := sha256.Sum256([]byte(kind + "\n" + normalized))
	return hex.EncodeToString(sum[:])[:hashLength]
}
