// Package attrib matches file events to the process that caused them.
//
// Correlation compares a file event against per-process write volume. It needs
// no privileges and it is wrong whenever another process wrote more bytes in the
// same window, which for small-file workloads is always. Causal attribution
// takes the writer from an operating-system event source instead: it is
// authoritative, but it needs elevation, so Configure attaches one and falls
// back to correlation whenever the mechanism cannot be used.
package attrib

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type record struct {
	name  string
	ppid  int32
	exe   string
	at    time.Time
	delta uint64
}

// Attributor holds recent per-process write activity and, when one is
// configured, a causal source that reports writers directly.
type Attributor struct {
	mu     sync.RWMutex
	recent map[int32]record
	window time.Duration
	trace  *tracer

	mode    string
	log     *slog.Logger
	source  Source
	causal  *causalIndex
	pending *pendingQueue
	delay   time.Duration

	causalHits   atomic.Uint64
	causalMisses atomic.Uint64
	pendingDrops atomic.Uint64
	degraded     atomic.Bool
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
		mode:   modeCorrelate,
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

// Suspect returns the most likely writer of path and a confidence in [0,1].
//
// A causal hit is authoritative and reports confidence 1.0. Otherwise the answer
// is the largest recent writer: confidence is 1.0 when exactly one process was
// writing, and the top writer's share of recent write volume when several were.
// A low confidence means the caller should prefer tree-level attribution over
// blaming a single process.
func (a *Attributor) Suspect(now time.Time, path string) (pid int32, name string, ppid int32, exe string, confidence float64) {
	if a.causal != nil {
		if w, ok := a.causal.match(path, now); ok {
			a.causalHits.Add(1)
			name, ppid, exe = a.identity(w)
			a.trace.record(newDecision(now, w.PID, name, 1, 0, 0, 1, sourceCausal))
			return w.PID, name, ppid, exe, 1
		}
		a.causalMisses.Add(1)
	}

	pid, name, ppid, exe, confidence = a.correlate(now)
	return pid, name, ppid, exe, confidence
}

// correlate blames the process with the largest recent write volume.
func (a *Attributor) correlate(now time.Time) (pid int32, name string, ppid int32, exe string, confidence float64) {
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
		a.trace.record(newDecision(now, 0, "", candidat, 0, total, 0, sourceCorrelate))
		return 0, "", 0, "", 0
	}
	if candidat == 1 {
		confidence = 1
	} else {
		confidence = float64(top.delta) / float64(total)
	}

	a.trace.record(newDecision(now, topPID, top.name, candidat, top.delta, total, confidence, sourceCorrelate))
	return topPID, top.name, top.ppid, top.exe, confidence
}

// identity fills what the causal source does not report from the last process
// sample, so tree aggregation still sees the writer's parent.
func (a *Attributor) identity(w Writer) (name string, ppid int32, exe string) {
	name, exe = w.Name, w.Exe

	a.mu.RLock()
	rec, known := a.recent[w.PID]
	a.mu.RUnlock()

	if !known {
		return name, 0, exe
	}
	if name == "" {
		name = rec.name
	}
	if exe == "" {
		exe = rec.exe
	}
	return name, rec.ppid, exe
}

// Forget drops a process's activity, so a reaped PID is not blamed for a
// recycled one.
func (a *Attributor) Forget(pid int32) {
	a.mu.Lock()
	delete(a.recent, pid)
	a.mu.Unlock()
}

// Stats reports how each mechanism is doing. A causal mode whose source has
// stopped delivering is visible here rather than only in the log.
type Stats struct {
	Mode         string
	CausalHits   uint64
	CausalMisses uint64
	PendingDrops uint64
	Degraded     bool
	Source       SourceStats
}

// Stats returns a snapshot of attribution health.
func (a *Attributor) Stats() Stats {
	if a == nil {
		return Stats{}
	}
	stats := Stats{
		Mode:         a.mode,
		CausalHits:   a.causalHits.Load(),
		CausalMisses: a.causalMisses.Load(),
		PendingDrops: a.pendingDrops.Load(),
		Degraded:     a.degraded.Load(),
	}
	if a.source != nil {
		stats.Source = a.source.Stats()
	}
	return stats
}

// Close stops the causal source and releases the decision trace, if one is open.
// It reports any decisions the trace failed to write, so a run whose measurement
// is incomplete says so rather than reporting a number that quietly under-counts.
func (a *Attributor) Close() error {
	if a == nil {
		return nil
	}
	a.stopCausal()
	return a.trace.close()
}
