// Package attrib guesses which process caused a file event.
//
// User-space file notification APIs report that a file changed, not who changed
// it. This correlator matches file events against per-process write counters
// sampled by the process sensor, and reports a confidence so callers can tell a
// clear match from a guess.
package attrib

import (
	"sync"
	"time"
)

type record struct {
	name  string
	ppid  int32
	exe   string
	at    time.Time
	delta uint64
}

// Attributor holds recent per-process write activity.
type Attributor struct {
	mu     sync.RWMutex
	recent map[int32]record
	window time.Duration
	trace  *tracer
}

// New returns an attributor that only considers activity within window. It
// starts a decision trace when GRIMA_ATTRIB_TRACE names a file.
func New(window time.Duration) *Attributor {
	if window <= 0 {
		window = 2 * time.Second
	}
	return &Attributor{
		recent: make(map[int32]record),
		window: window,
		trace:  newTracerFromEnv(),
	}
}

// Observe records that a process wrote delta bytes since the last sample.
func (a *Attributor) Observe(pid int32, name string, ppid int32, exe string, delta uint64) {
	if pid == 0 || delta == 0 {
		return
	}
	a.mu.Lock()
	a.recent[pid] = record{name: name, ppid: ppid, exe: exe, at: time.Now(), delta: delta}
	a.mu.Unlock()
}

// Suspect returns the most likely writer and a confidence in [0,1].
//
// Confidence is 1.0 when exactly one process was writing, and the top writer's
// share of recent write volume otherwise. A low confidence means the caller
// should prefer tree-level attribution over blaming a single process.
func (a *Attributor) Suspect(now time.Time) (pid int32, name string, ppid int32, exe string, confidence float64) {
	a.mu.RLock()

	var (
		topPID   int32
		top      record
		total    uint64
		candidat int
	)
	for id, rec := range a.recent {
		if now.Sub(rec.at) > a.window {
			continue
		}
		candidat++
		total += rec.delta
		if rec.delta > top.delta {
			top = rec
			topPID = id
		}
	}

	a.mu.RUnlock()

	if candidat == 0 || total == 0 {
		a.trace.record(newDecision(now, 0, "", candidat, 0, total, 0))
		return 0, "", 0, "", 0
	}
	if candidat == 1 {
		confidence = 1
	} else {
		confidence = float64(top.delta) / float64(total)
	}

	a.trace.record(newDecision(now, topPID, top.name, candidat, top.delta, total, confidence))
	return topPID, top.name, top.ppid, top.exe, confidence
}

// Forget drops a process's activity, so a reaped PID is not blamed for a
// recycled one.
func (a *Attributor) Forget(pid int32) {
	a.mu.Lock()
	delete(a.recent, pid)
	a.mu.Unlock()
}

// Close releases the decision trace, if one is open. It reports any decisions
// the trace failed to write, so a run whose measurement is incomplete says so
// rather than reporting a number that quietly under-counts.
func (a *Attributor) Close() error {
	if a == nil {
		return nil
	}
	return a.trace.close()
}
