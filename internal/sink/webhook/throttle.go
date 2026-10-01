package webhook

import (
	"sync"
	"time"
)

// throttle allows at most one notification per (label, fingerprint) per window.
// It replaces the incident state machine milestone 02 will add, so a crash loop
// cannot flood a channel before that exists. The incident count keeps rising in
// the tracker regardless; this only gates outbound delivery (RT-WH-8).
type throttle struct {
	mu     sync.Mutex
	window time.Duration
	last   map[string]time.Time
}

func newThrottle(window time.Duration) *throttle {
	return &throttle{window: window, last: make(map[string]time.Time)}
}

// allow reports whether a notification for this incident may be delivered now,
// recording the time when it may. A non-positive window disables throttling.
func (t *throttle) allow(label, fingerprint string, now time.Time) bool {
	if t.window <= 0 {
		return true
	}
	key := label + "\x00" + fingerprint

	t.mu.Lock()
	defer t.mu.Unlock()
	if last, ok := t.last[key]; ok && now.Sub(last) < t.window {
		return false
	}
	t.last[key] = now
	return true
}
