package webhook

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/shaheeranser/watcher/internal/sink"
)

// spool appends undeliverable notifications, one JSON line each, to a local
// file so a wedged receiver does not lose the report (RT-WH-6). Draining the
// spool is deliberately not automated here.
type spool struct {
	mu   sync.Mutex
	path string
}

func newSpool(path string) *spool {
	return &spool{path: path}
}

// append writes one notification and the reason it could not be delivered.
func (s *spool) append(r sink.Result, deliveryErr error) error {
	entry := genericPayloadFor(r)
	if deliveryErr != nil {
		entry.DeliveryError = deliveryErr.Error()
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(line)
	return err
}
