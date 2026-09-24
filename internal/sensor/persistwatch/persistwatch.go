// Package persistwatch detects new persistence mechanisms.
//
// Persistence is often established before mass encryption, so this is the signal
// class most likely to fire first. What counts as persistence is platform
// specific and lives in the build-tagged files beside this one.
package persistwatch

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
	"github.com/prateekpurohit13/grima/internal/sensor"
)

const (
	name         = "persistwatch"
	pollInterval = 10 * time.Second
)

// entry is one persistence location on this host.
type entry struct {
	path string
	kind string
}

// Source polls the platform's persistence locations.
type Source struct {
	cfg config.Config
	log *slog.Logger

	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	seen map[string]struct{}

	events  atomic.Uint64
	errors  atomic.Uint64
	dropped atomic.Uint64
	// tracked mirrors len(seen) so Stats can be read from another goroutine
	// without touching the map, which only the poll loop may write.
	tracked atomic.Int64
}

// New returns a persistence sensor.
func New(cfg config.Config, log *slog.Logger) *Source {
	return &Source{
		cfg:  cfg,
		log:  log,
		done: make(chan struct{}),
		seen: make(map[string]struct{}),
	}
}

func (s *Source) Name() string { return name }

// Start records the current state as the baseline, so only new persistence is
// reported. It fails rather than starting blind if nothing can be read.
func (s *Source) Start(ctx context.Context, out chan<- event.Event) error {
	entries, err := scanPersistence()
	if err != nil {
		return err
	}
	for _, e := range entries {
		s.seen[e.path] = struct{}{}
	}
	s.tracked.Store(int64(len(s.seen)))

	s.wg.Add(1)
	go s.loop(ctx, out)
	return nil
}

// Close stops polling. Idempotent.
func (s *Source) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.wg.Wait()
	})
	return nil
}

// Stats reports sensor health.
func (s *Source) Stats() sensor.Stats {
	return sensor.Stats{
		Name:    name,
		Events:  s.events.Load(),
		Errors:  s.errors.Load(),
		Dropped: s.dropped.Load(),
		Extra:   map[string]uint64{"tracked": uint64(s.tracked.Load())},
	}
}

func (s *Source) loop(ctx context.Context, out chan<- event.Event) {
	defer s.wg.Done()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case <-ticker.C:
			s.poll(out)
		}
	}
}

func (s *Source) poll(out chan<- event.Event) {
	entries, err := scanPersistence()
	if err != nil {
		s.errors.Add(1)
		s.log.Warn("persistence scan failed", "source", name, "error", err)
		return
	}

	now := time.Now()
	for _, e := range entries {
		if _, known := s.seen[e.path]; known {
			continue
		}
		s.seen[e.path] = struct{}{}
		s.emit(out, event.Event{
			Kind:             event.KindPersistenceInstall,
			Time:             now,
			Path:             e.path,
			Persist:          e.kind,
			PID:              0,
			AttribConfidence: 0,
		})
	}

	s.tracked.Store(int64(len(s.seen)))
}

func (s *Source) emit(out chan<- event.Event, ev event.Event) {
	ev.Source = name
	s.events.Add(1)

	select {
	case out <- ev:
	default:
		s.dropped.Add(1)
	}
}
