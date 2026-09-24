// Package sensor defines the interface every event producer implements.
package sensor

import (
	"context"

	"github.com/prateekpurohit13/grima/internal/event"
)

// Source is one event producer: a filesystem watcher, a process watcher, or a
// persistence watcher.
//
// Contract:
//
//   - Start returns once the source is observing, not when it finishes.
//   - The source writes to out until ctx is cancelled or Close is called.
//   - Close is safe to call more than once.
//   - A source that cannot observe on this host returns an error from Start and
//     is skipped by the caller. It must never return nil and then produce
//     nothing: a silently blind sensor makes the detector look healthy.
//   - A source never blocks indefinitely on out. When out is full it drops and
//     counts the loss.
type Source interface {
	Name() string
	Start(ctx context.Context, out chan<- event.Event) error
	Close() error
}

// Stats is a sensor's self-reported health.
type Stats struct {
	Name    string
	Events  uint64
	Errors  uint64
	Dropped uint64
	Extra   map[string]uint64

	// Reporting says whether the source exposed counters at all. False means the
	// counters above are absent, not zero: a reader must not mistake a source
	// that cannot report for one that is running quietly.
	Reporting bool
}

// Reporter is implemented by sources that expose health counters. Sources that
// do not implement it are reported as unknown rather than healthy.
type Reporter interface {
	Stats() Stats
}
