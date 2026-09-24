package procwatch

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/prateekpurohit13/grima/internal/attrib"
	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
)

func testSource(interval time.Duration) *Source {
	cfg := config.Default()
	cfg.ProcWatch.SampleInterval = config.Duration(interval)
	return New(cfg, attrib.New(cfg.Window.DecayHalfLife.Std()), slog.New(slog.DiscardHandler))
}

// A source that started must count what it emits: /healthz reads this counter,
// and a started sensor reporting zero events looks the same as a blind one.
func TestStartedSourceCountsEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := testSource(10 * time.Millisecond)
	out := make(chan event.Event, 1<<12)
	if err := src.Start(ctx, out); err != nil {
		t.Fatalf("start source: %v", err)
	}
	defer src.Close()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-out:
			if ev.Kind != event.KindProcessStart {
				continue
			}
			if ev.Source != name {
				t.Fatalf("event source = %q, want %q", ev.Source, name)
			}
			stats := src.Stats()
			if stats.Name != name {
				t.Fatalf("stats name = %q, want %q", stats.Name, name)
			}
			if stats.Events == 0 {
				t.Fatal("source emitted an event but reports 0 events")
			}
			return
		case <-deadline:
			t.Fatal("no process start event within 5s")
		}
	}
}

// A full output channel must cost dropped events, never a stalled sensor.
func TestFullOutputDropsAreCounted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := testSource(10 * time.Millisecond)
	// Never read from this channel, so every emit after the first must drop.
	out := make(chan event.Event, 1)
	if err := src.Start(ctx, out); err != nil {
		t.Fatalf("start source: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for src.Stats().Dropped == 0 {
		select {
		case <-deadline:
			t.Fatalf("no drop counted within 5s, events=%d", src.Stats().Events)
		case <-time.After(10 * time.Millisecond):
		}
	}

	closed := make(chan struct{})
	go func() {
		src.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked while the output channel was full")
	}
}

// Stats is read from the HTTP handler while the sample loop mutates the process
// map. Reading it must not touch that map: a concurrent map read and write is a
// fatal error, not a race the detector can recover from, so this runs under
// `make race` as the regression test for it.
func TestStatsIsSafeToReadWhileSampling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := testSource(2 * time.Millisecond)
	out := make(chan event.Event, 1<<12)
	if err := src.Start(ctx, out); err != nil {
		t.Fatalf("start source: %v", err)
	}
	defer src.Close()

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if stats := src.Stats(); stats.Name != name {
					return
				}
			}
		}
	}()

	// Let the sampler take many ticks while the reader above runs.
	time.Sleep(300 * time.Millisecond)
	cancel()
	<-readerDone
}
