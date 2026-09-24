package attrib

import (
	"strings"
	"sync"
	"time"
)

// Writer is a file write the operating system attributed to a process. It is
// the causal answer to "who wrote this", as opposed to a guess from volume.
type Writer struct {
	Path string
	PID  int32
	Name string
	Exe  string
	At   time.Time
}

// Source is an operating-system mechanism that reports the process behind a
// file write. A source is started once and delivers until it is closed.
type Source interface {
	// Start begins delivering writers to record. It returns an error when the
	// mechanism cannot be used at all, such as when the process is not elevated.
	Start(record func(Writer)) error
	// Describe names the mechanism and its coverage, for the startup log.
	Describe() string
	// Stats reports what the source has seen since it started.
	Stats() SourceStats
	// Close stops delivery. Idempotent.
	Close() error
}

// SourceStats reports a causal source's own counters.
type SourceStats struct {
	Records uint64 // writers handed to the index
	Skipped uint64 // events that were not a file write by a known path
	Errors  uint64 // delivery, render, or parse failures
}

const (
	// causalPerPath bounds how many writes to one path are remembered, so a
	// single hot file cannot grow the index.
	causalPerPath = 4
	// causalMaxPaths bounds the index. A record that would exceed it is dropped
	// and counted; the file event it belonged to falls back to correlation.
	causalMaxPaths = 32768
	// causalPruneEvery is how many records may arrive between expired-path sweeps.
	causalPruneEvery = 256
	// causalClockSkew is how far a record's timestamp may sit after the file
	// change it explains. File-system auditing reports a handle when it closes,
	// which is after the write that the directory notification reports.
	causalClockSkew = 2 * time.Second
)

// causalIndex holds recent OS-reported writers, keyed by path, so a file event
// can ask who wrote it. It is safe for concurrent use.
type causalIndex struct {
	mu     sync.Mutex
	notify chan struct{}
	byPath map[string][]Writer
	window time.Duration
	selfID int32

	records uint64
	dropped uint64
	ignored uint64
}

// newCausalIndex returns an index that answers questions about changes within
// window and ignores writers that are this process itself.
func newCausalIndex(window time.Duration, selfID int32) *causalIndex {
	return &causalIndex{
		notify: make(chan struct{}),
		byPath: make(map[string][]Writer),
		window: window,
		selfID: selfID,
	}
}

// record files one OS-reported write. It is called from the source's delivery
// goroutine, so it never blocks.
func (i *causalIndex) record(w Writer) {
	if w.Path == "" || w.PID == 0 || w.PID == i.selfID {
		i.mu.Lock()
		i.ignored++
		i.mu.Unlock()
		return
	}

	key := causalKey(w.Path)

	i.mu.Lock()
	defer i.mu.Unlock()

	if i.records%causalPruneEvery == 0 {
		i.pruneLocked()
	}
	if len(i.byPath) >= causalMaxPaths {
		if _, tracked := i.byPath[key]; !tracked {
			i.dropped++
			return
		}
	}

	history := append(i.byPath[key], w)
	if len(history) > causalPerPath {
		history = history[len(history)-causalPerPath:]
	}
	i.byPath[key] = history
	i.records++

	// Wake every waiter: a new record may be the one it is waiting for.
	close(i.notify)
	i.notify = make(chan struct{})
}

// match returns the newest record for path that could explain a change observed
// at at, without blocking.
func (i *causalIndex) match(path string, at time.Time) (Writer, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.matchLocked(causalKey(path), at)
}

// await blocks until a record for path arrives or maxWait elapses, and reports
// whether the index now holds one.
func (i *causalIndex) await(path string, at time.Time, maxWait time.Duration) bool {
	key := causalKey(path)

	deadline := time.NewTimer(maxWait)
	defer deadline.Stop()

	for {
		i.mu.Lock()
		_, ok := i.matchLocked(key, at)
		wake := i.notify
		i.mu.Unlock()
		if ok {
			return true
		}

		select {
		case <-wake:
		case <-deadline.C:
			i.mu.Lock()
			_, ok := i.matchLocked(key, at)
			i.mu.Unlock()
			return ok
		}
	}
}

// counters reports the index's own counters.
func (i *causalIndex) counters() (records, dropped, ignored uint64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.records, i.dropped, i.ignored
}

func (i *causalIndex) matchLocked(key string, at time.Time) (Writer, bool) {
	oldest := at.Add(-i.window)
	newest := at.Add(causalClockSkew)

	history := i.byPath[key]
	for j := len(history) - 1; j >= 0; j-- {
		w := history[j]
		if w.At.Before(oldest) || w.At.After(newest) {
			continue
		}
		return w, true
	}
	return Writer{}, false
}

// pruneLocked drops paths whose newest record is older than the window, so the
// index tracks recent writes rather than the uptime of the detector.
func (i *causalIndex) pruneLocked() {
	cutoff := time.Now().Add(-i.window)
	for key, history := range i.byPath {
		if len(history) == 0 || history[len(history)-1].At.Before(cutoff) {
			delete(i.byPath, key)
		}
	}
}

// causalKey normalizes a path for lookup. Windows paths are case-insensitive,
// and the audited path and the watched path can disagree on case.
func causalKey(path string) string {
	return strings.ToLower(strings.TrimSpace(path))
}
