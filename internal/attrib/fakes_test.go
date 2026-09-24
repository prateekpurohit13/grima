package attrib

import (
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prateekpurohit13/grima/internal/event"
)

// fakeSource is a causal source the tests drive by hand, so a mechanism that
// needs elevation can be exercised without it.
type fakeSource struct {
	mu     sync.Mutex
	record func(Writer)
	start  error
	closed bool
	stats  SourceStats
}

func (f *fakeSource) Start(record func(Writer)) error {
	if f.start != nil {
		return f.start
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record = record
	return nil
}

func (f *fakeSource) deliver(w Writer) {
	f.mu.Lock()
	record := f.record
	f.mu.Unlock()
	if record != nil {
		record(w)
	}
}

func (f *fakeSource) Describe() string { return "fake causal source" }

func (f *fakeSource) Stats() SourceStats {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats
}

func (f *fakeSource) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func testLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// causalAttributor returns an attributor with a fake causal source attached.
func causalAttributor(t *testing.T, delay time.Duration) (*Attributor, *fakeSource) {
	t.Helper()

	a := New(time.Second)
	a.log = testLogger()
	src := &fakeSource{}
	if err := a.attachSource(src, delay); err != nil {
		t.Fatalf("attach fake source: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a, src
}

// emitRecorder collects emitted events.
type emitRecorder struct {
	mu     sync.Mutex
	events []event.Event
	done   chan struct{}
	want   int
}

func newEmitRecorder(want int) *emitRecorder {
	return &emitRecorder{done: make(chan struct{}), want: want}
}

func (r *emitRecorder) emit(ev event.Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	complete := len(r.events) >= r.want
	r.mu.Unlock()
	if complete {
		select {
		case <-r.done:
		default:
			close(r.done)
		}
	}
}

func (r *emitRecorder) wait(t *testing.T) []event.Event {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("only %d of %d events were emitted", len(r.collected()), r.want)
	}
	return r.collected()
}

func (r *emitRecorder) collected() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.Event(nil), r.events...)
}
