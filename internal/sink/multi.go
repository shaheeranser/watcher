package sink

import (
	"context"
	"errors"
	"fmt"
)

// Multi delivers each result to several sinks independently, so one failing
// sink never suppresses the others (RT-WH-9). It reports the failures it saw,
// but only after every sink has had the result.
type Multi struct {
	sinks []Sink
}

func NewMulti(sinks ...Sink) *Multi {
	return &Multi{sinks: sinks}
}

func (m *Multi) Name() string { return "multi" }

func (m *Multi) Emit(ctx context.Context, r Result) error {
	var errs []error
	for _, s := range m.sinks {
		if err := s.Emit(ctx, r); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
		}
	}
	return errors.Join(errs...)
}
