package attrib

import (
	"sync"
	"sync/atomic"

	"github.com/prateekpurohit13/grima/internal/event"
)

// pendingQueue carries file events that are waiting for a causal record off the
// sensor's goroutine. It is bounded, so a busy directory cannot stall the sensor
// or grow the detector's memory.
type pendingQueue struct {
	items  chan pendingEvent
	done   chan struct{}
	closed atomic.Bool
	wg     sync.WaitGroup
}

type pendingEvent struct {
	ev   event.Event
	emit func(event.Event)
}

const (
	// pendingCapacity is how many file events may wait at once. At the default
	// delay this covers thousands of events per second; beyond it the sensor
	// keeps moving by falling back to correlation.
	pendingCapacity = 512
	// causalMissStreak is how many events in a row may go unattributed before
	// the attributor stops waiting. A source that never delivers would otherwise
	// slow every event for the rest of the run.
	causalMissStreak = 20
)

// Resolve attributes ev and calls emit once the writer is known.
//
// In correlation mode this is the synchronous path the detector has always used.
// In a causal mode the event waits, off the sensor's goroutine, for the OS to
// report the writer, and is attributed by correlation if that report does not
// arrive in time. Resolve never blocks the caller.
func (a *Attributor) Resolve(ev event.Event, emit func(event.Event)) {
	if a == nil {
		emit(ev)
		return
	}

	// Host mode: no per-process claim is made, so the event keeps PID 0 and is
	// filed against the host fingerprint. Guessing here would spread one slow
	// drip across whichever processes happened to be busy, and the cumulative
	// track — the design's answer to drip encryption — would never accumulate.
	if a.mode == modeHost {
		emit(ev)
		return
	}

	if a.pending == nil {
		a.attribute(&ev)
		emit(ev)
		return
	}
	if !a.pending.push(pendingEvent{ev: ev, emit: emit}) {
		a.pendingDrops.Add(1)
		a.attribute(&ev)
		emit(ev)
	}
}

// attribute fills the actor fields from the best available mechanism.
func (a *Attributor) attribute(ev *event.Event) {
	pid, name, ppid, exe, confidence := a.Suspect(ev.Time, ev.Path)
	ev.PID = pid
	ev.ProcName = name
	ev.PPID = ppid
	ev.Exe = exe
	ev.AttribConfidence = confidence
}

// push queues one event. It reports false when the queue is full or the
// detector is shutting down, and the caller attributes the event inline instead.
func (q *pendingQueue) push(item pendingEvent) bool {
	if q.closed.Load() {
		return false
	}
	select {
	case q.items <- item:
		return true
	default:
		return false
	}
}

// startPendingQueue starts the resolver that turns queued file events into
// emitted ones.
func startPendingQueue(a *Attributor) *pendingQueue {
	q := &pendingQueue{
		items: make(chan pendingEvent, pendingCapacity),
		done:  make(chan struct{}),
	}
	q.wg.Add(1)
	go a.resolveLoop(q)
	return q
}

// close stops the resolver and reports how many queued events it never
// resolved. It is idempotent.
func (q *pendingQueue) close() uint64 {
	if q.closed.Swap(true) {
		return 0
	}
	close(q.done)
	q.wg.Wait()
	return q.drain()
}

// drain counts the events still queued when the detector stops.
func (q *pendingQueue) drain() uint64 {
	var left uint64
	for {
		select {
		case <-q.items:
			left++
		default:
			return left
		}
	}
}

func (a *Attributor) resolveLoop(q *pendingQueue) {
	defer q.wg.Done()

	misses := 0
	for {
		select {
		case <-q.done:
			return
		case item := <-q.items:
			misses = a.resolve(item, misses)
		}
	}
}

// resolve attributes one queued event and emits it. It returns the run of
// consecutive events that had no causal record.
func (a *Attributor) resolve(item pendingEvent, misses int) int {
	ev := item.ev

	// While degraded the resolver does not wait, but it still looks: a record
	// that arrives on its own is proof the source is alive again.
	var attributed bool
	if a.degraded.Load() {
		_, attributed = a.causal.match(ev.Path, ev.Time)
	} else {
		attributed = a.causal.await(ev.Path, ev.Time, a.delay)
	}

	if attributed {
		if a.degraded.Swap(false) {
			a.log.Info("causal attribution recovered")
		}
		misses = 0
	} else {
		misses++
		if misses >= causalMissStreak && !a.degraded.Swap(true) {
			a.log.Warn("causal attribution has stopped delivering; correlating without waiting",
				"mode", a.mode, "misses", misses)
		}
	}

	a.attribute(&ev)
	item.emit(ev)
	return misses
}

// stopCausal stops the resolver and the source. Events still queued are dropped
// and counted: the detector is shutting down, and a drop must never be silent.
//
// The source and the queue stay in place, because the health endpoint reads them
// from another goroutine. They are stopped, not removed.
func (a *Attributor) stopCausal() {
	if a.pending != nil {
		if left := a.pending.close(); left > 0 {
			a.pendingDrops.Add(left)
		}
	}
	if a.source != nil {
		if err := a.source.Close(); err != nil {
			a.log.Warn("closing causal source", "mode", a.mode, "error", err)
		}
	}
	if a.pendingDrops.Load() > 0 {
		a.log.Warn("file events left unattributed at shutdown", "count", a.pendingDrops.Load())
	}
}
