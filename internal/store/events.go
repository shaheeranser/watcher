package store

import "sync"

// EventType names a mutation on the incident stream.
type EventType string

const (
	// EventCreated is the first occurrence of an incident (or a fresh cycle
	// after resolution).
	EventCreated EventType = "created"

	// EventCounted is a further occurrence of an existing incident.
	EventCounted EventType = "counted"

	// EventExplained is an explanation arriving for an incident.
	EventExplained EventType = "explained"

	// EventStateChanged is a lifecycle transition, currently only to resolved.
	EventStateChanged EventType = "state_changed"
)

// Event is a small mutation notice. It carries the identity and the changed
// field set, not the full row, so the client fetches detail lazily and a stream
// of events cannot balloon a client's memory (design §2.3).
type Event struct {
	Type        EventType `json:"type"`
	ID          string    `json:"id"`
	Fingerprint string    `json:"fingerprint"`
	Source      string    `json:"source"`
	Count       int       `json:"count"`
	State       string    `json:"state"`
	Fields      []string  `json:"fields,omitempty"`
}

// subscriberBuffer bounds one client's backlog. A subscriber that cannot keep up
// drops events rather than stalling the writer or another client (DASH-NFR-3).
const subscriberBuffer = 64

// broker fans committed mutations out to SSE subscribers. Its publish path is
// non-blocking on purpose: a stalled reader must never block the daemon
// (DASH-NFR-3, DASH-21).
type broker struct {
	mu     sync.Mutex
	next   int
	subs   map[int]chan Event
	closed bool
}

func newBroker() *broker {
	return &broker{subs: make(map[int]chan Event)}
}

// subscribe registers a subscriber and returns its channel plus the function
// that removes it.
func (b *broker) subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	id := b.next
	b.next++
	ch := make(chan Event, subscriberBuffer)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(ch)
		}
	}
}

// publish delivers an event to every subscriber, dropping it for any whose
// buffer is full.
func (b *broker) publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (b *broker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, ch := range b.subs {
		delete(b.subs, id)
		close(ch)
	}
}
