package incident

import (
	"sync"
	"time"
)

// NotificationKind is the lifecycle stage a notification announces. It is the
// vocabulary the state machine applies to every crash loop: tell someone once
// when it starts, at most once a window while it continues, and once when it
// stops.
type NotificationKind string

const (
	// KindNew is the first notification for a fingerprint. It is emitted
	// immediately, before the explanation is ready (PROD-STM-1).
	KindNew NotificationKind = "new"

	// KindOngoing is a throttled "still happening" update, or the follow-up
	// that carries an explanation once the model returns (PROD-STM-2,
	// PROD-STM-8).
	KindOngoing NotificationKind = "ongoing"

	// KindResolved is the single notification after a fingerprint has been
	// quiet for the resolve window (PROD-STM-3).
	KindResolved NotificationKind = "resolved"
)

// Observation is what the state machine decided about one occurrence. Counting
// is the tracker's job; this only answers whether to tell anyone, and what.
type Observation struct {
	// Notify reports that a notification is due now. A suppressed repeat
	// leaves it false while the tracker still records the occurrence.
	Notify bool

	// Kind is KindNew or KindOngoing when Notify is set.
	Kind NotificationKind

	// Fresh reports that this occurrence reopened an incident that had been
	// resolved, so its cycle (and its count) restarts.
	Fresh bool
}

// Notification identifies an incident a notification refers to. The engine
// joins it with the tracker's incident to build the emitted result.
type Notification struct {
	Label       string
	Fingerprint string
	Kind        NotificationKind
}

// Notifier is the incident state machine. It is separate from the Tracker
// because it answers a different question: the tracker says how many times and
// when, while this decides whether anyone should be told right now. Keeping the
// policy out of the counts is what lets a read-only consumer (the milestone-03
// TUI) use incident history without reasoning about throttling.
type Notifier struct {
	mu sync.Mutex

	// throttle is T: the minimum interval between ongoing notifications for
	// one incident. resolve is W: how long an incident must be quiet before it
	// is announced resolved.
	throttle time.Duration
	resolve  time.Duration

	states map[key]*notifyState
}

// notifyState is the process-local policy state for one (label, fingerprint).
// Restarting the process discards it, which is what makes a restart unable to
// emit a spurious resolved notification for a fingerprint it no longer knows.
type notifyState struct {
	lastSeen     time.Time
	lastNotified time.Time

	// pending records that the most recent notification went out before the
	// explanation was available, so one follow-up is owed.
	pending bool

	resolved bool
}

// NewNotifier builds a state machine with the throttle window T and resolve
// window W. Both are expected to be positive; a non-positive resolve window
// disables resolution.
func NewNotifier(throttle, resolve time.Duration) *Notifier {
	return &Notifier{throttle: throttle, resolve: resolve, states: make(map[key]*notifyState)}
}

// Observe records one occurrence of a (label, fingerprint) crash and reports
// the notification policy's decision. Repeats inside the throttle window are
// suppressed here only for delivery; the tracker has already counted them
// (PROD-STM-2, PROD-STM-5). An occurrence after resolution starts a fresh cycle
// (PROD-STM-4).
func (n *Notifier) Observe(label, fingerprint string, now time.Time) Observation {
	n.mu.Lock()
	defer n.mu.Unlock()

	k := key{label: label, fingerprint: fingerprint}
	st, ok := n.states[k]
	if !ok || st.resolved {
		n.states[k] = &notifyState{
			lastSeen:     now,
			lastNotified: now,
			pending:      true,
		}
		return Observation{Notify: true, Kind: KindNew, Fresh: true}
	}

	st.lastSeen = now
	if now.Sub(st.lastNotified) < n.throttle {
		return Observation{}
	}
	st.lastNotified = now
	return Observation{Notify: true, Kind: KindOngoing}
}

// Explained reports whether the arrival of an explanation owes a follow-up
// notification. A notification sent before the model returned carries a pending
// marker; this delivers the explanation without holding the original alert
// (PROD-STM-8). It fires at most once per outstanding claim.
func (n *Notifier) Explained(label, fingerprint string, now time.Time) (NotificationKind, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	st, ok := n.states[key{label: label, fingerprint: fingerprint}]
	if !ok || st.resolved || !st.pending {
		return "", false
	}
	st.pending = false
	st.lastNotified = now
	return KindOngoing, true
}

// Resolve emits the single resolved notification for every incident that has
// been quiet for the resolve window, and marks them resolved so a later
// occurrence starts a new cycle (PROD-STM-3, PROD-STM-4). Incidents the process
// is not tracking are never resolved, so a restart cannot produce a spurious
// resolved burst (PROD-STM-7).
func (n *Notifier) Resolve(now time.Time) []Notification {
	if n.resolve <= 0 {
		return nil
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	var out []Notification
	for k, st := range n.states {
		if st.resolved || now.Sub(st.lastSeen) < n.resolve {
			continue
		}
		st.resolved = true
		st.pending = false
		out = append(out, Notification{Label: k.label, Fingerprint: k.fingerprint, Kind: KindResolved})
	}
	return out
}
