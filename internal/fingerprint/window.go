// Package fingerprint folds raw events into per-process behavioral fingerprints
// and aggregates them over process trees.
package fingerprint

import (
	"time"

	"github.com/prateekpurohit13/grima/internal/event"
)

// ringCapacity bounds retained samples per process, capping memory at
// (live processes x ringCapacity) regardless of uptime.
const ringCapacity = 4096

type sample struct {
	at      time.Time
	kind    event.Kind
	bytes   int64
	dir     string
	ext     string
	entropy float64
	magic   bool
}

type procState struct {
	name      string
	ppid      int32
	firstSeen time.Time
	lastSeen  time.Time

	ring  []sample
	head  int
	count int

	// Non-decaying counters. These never age out, which is what catches drip
	// encryption that no single window would ever see.
	cumFilesRewritten int64
	cumBytesRewritten int64
	extActivity       map[string]int64
}

func newProcState(name string, ppid int32, at time.Time) *procState {
	return &procState{
		name:        name,
		ppid:        ppid,
		firstSeen:   at,
		lastSeen:    at,
		ring:        make([]sample, ringCapacity),
		extActivity: make(map[string]int64),
	}
}

func (p *procState) push(s sample) {
	p.ring[p.head] = s
	p.head = (p.head + 1) % len(p.ring)
	if p.count < len(p.ring) {
		p.count++
	}
	p.lastSeen = s.at
}

// each iterates newest-first and stops when fn returns false.
func (p *procState) each(fn func(sample) bool) {
	n := len(p.ring)
	for i := range p.count {
		idx := (p.head - 1 - i + n) % n
		if !fn(p.ring[idx]) {
			return
		}
	}
}

// EntropySample is one observed entropy value tagged with its extension, so the
// scorer can compare it against the matching per-extension baseline.
type EntropySample struct {
	Ext string
	H   float64
}

// TreeVector is the summed fingerprint of a process and all its descendants.
// Scoring the tree rather than the process is what defeats workload splitting.
type TreeVector struct {
	Root     int32
	ProcName string
	PIDs     []int32

	Writes        int
	Creates       int
	Renames       int
	Deletes       int
	Bytes         int64
	Dirs          map[string]struct{}
	Entropy       []EntropySample
	MagicTotal    int
	MagicMismatch int

	CumFilesRewritten int64
	CumBytesRewritten int64
	ExtActivity       map[string]int64

	WindowDuration time.Duration
}

// DirCount returns the number of distinct directories touched in the window.
func (t TreeVector) DirCount() int { return len(t.Dirs) }

// Window is a read-only snapshot of one process fingerprint.
type Window struct {
	Writes        int
	Creates       int
	Renames       int
	Deletes       int
	Bytes         int64
	Dirs          map[string]struct{}
	Entropy       []EntropySample
	MagicTotal    int
	MagicMismatch int

	CumFilesRewritten int64
	CumBytesRewritten int64
	ExtActivity       map[string]int64

	FirstSeen time.Time
	LastSeen  time.Time
}

// counts accumulates window counters. Shared by Aggregate and snapshot so the
// two can never drift apart.
type counts struct {
	writes   int
	creates  int
	renames  int
	deletes  int
	bytes    int64
	dirs     map[string]struct{}
	entropy  []EntropySample
	magicBad int
	magicAll int
}

func newCounts() *counts {
	return &counts{dirs: make(map[string]struct{})}
}

func (c *counts) add(s sample) {
	switch s.kind {
	case event.KindFileWrite:
		c.writes++
	case event.KindFileCreate:
		c.creates++
	case event.KindFileRename:
		c.renames++
	case event.KindFileDelete:
		c.deletes++
	}
	c.bytes += s.bytes
	if s.dir != "" {
		c.dirs[s.dir] = struct{}{}
	}
	if s.kind != event.KindFileWrite {
		return
	}
	c.magicAll++
	if s.magic {
		c.magicBad++
	}
	if s.entropy > 0 {
		c.entropy = append(c.entropy, EntropySample{Ext: s.ext, H: s.entropy})
	}
}
