package fingerprint

import (
	"fmt"
	"testing"
	"time"

	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
)

func testEngine(t *testing.T, halfLife time.Duration) *Engine {
	t.Helper()
	cfg := config.Default()
	cfg.Window.DecayHalfLife = config.Duration(halfLife)
	return NewEngine(cfg)
}

func write(pid int32, path string, bytes int64, at time.Time) event.Event {
	return event.Event{Kind: event.KindFileWrite, PID: pid, Path: path, Bytes: bytes, Time: at}
}

func TestAggregateSumsOverProcessTree(t *testing.T) {
	e := testEngine(t, time.Minute)
	now := time.Now()

	e.Apply(event.Event{Kind: event.KindProcessStart, PID: 10, ProcName: "parent", Time: now})
	e.Apply(event.Event{Kind: event.KindProcessStart, PID: 11, PPID: 10, ProcName: "child", Time: now})
	e.Apply(write(10, "/data/a.txt", 100, now))
	e.Apply(write(11, "/data/b.txt", 200, now))

	tv := e.Aggregate(10)

	if tv.Writes != 2 {
		t.Fatalf("writes = %d, want 2", tv.Writes)
	}
	if tv.Bytes != 300 {
		t.Fatalf("bytes = %d, want 300", tv.Bytes)
	}
	if len(tv.PIDs) != 2 {
		t.Fatalf("pids = %v, want parent and child", tv.PIDs)
	}
	if tv.DirCount() != 1 {
		t.Fatalf("directories = %d, want 1", tv.DirCount())
	}
}

// The point of the dual-track design: the window ages out, the counters do not.
func TestCumulativeCountersSurviveDecay(t *testing.T) {
	e := testEngine(t, 40*time.Millisecond)
	e.Apply(event.Event{Kind: event.KindProcessStart, PID: 5, ProcName: "dripper", Time: time.Now()})

	for i := range 10 {
		e.Apply(write(5, fmt.Sprintf("/data/f%d.txt", i), 10, time.Now()))
	}
	time.Sleep(120 * time.Millisecond)

	tv := e.Aggregate(5)
	if tv.Writes != 0 {
		t.Fatalf("window writes = %d, want 0 after decay", tv.Writes)
	}
	if tv.CumFilesRewritten != 10 {
		t.Fatalf("cumulative files = %d, want 10 (must not decay)", tv.CumFilesRewritten)
	}
	if tv.CumBytesRewritten != 100 {
		t.Fatalf("cumulative bytes = %d, want 100", tv.CumBytesRewritten)
	}
}

func TestExtensionActivityIsCumulative(t *testing.T) {
	e := testEngine(t, time.Minute)
	now := time.Now()
	e.Apply(event.Event{Kind: event.KindProcessStart, PID: 7, ProcName: "crypt", Time: now})

	for i := range 4 {
		ev := write(7, fmt.Sprintf("/data/doc%d.docx", i), 10, now)
		ev.Kind = event.KindFileCreate
		e.Apply(ev)
	}

	tv := e.Aggregate(7)
	if tv.ExtActivity[".docx"] != 4 {
		t.Fatalf("docx activity = %d, want 4", tv.ExtActivity[".docx"])
	}
}

// Sad path: an event with no attributable process must not create state.
func TestUnattributedEventsAreIgnored(t *testing.T) {
	e := testEngine(t, time.Minute)

	e.Apply(event.Event{Kind: event.KindFileWrite, PID: 0, Path: "/data/a.txt"})

	if e.Live() != 0 {
		t.Fatalf("live processes = %d, want 0", e.Live())
	}
}

func TestReapReleasesStateAndOrphansChild(t *testing.T) {
	e := testEngine(t, time.Minute)
	now := time.Now()
	e.Apply(event.Event{Kind: event.KindProcessStart, PID: 20, ProcName: "parent", Time: now})
	e.Apply(event.Event{Kind: event.KindProcessStart, PID: 21, PPID: 20, ProcName: "child", Time: now})

	e.Reap(20)

	if e.Live() != 1 {
		t.Fatalf("live processes = %d, want 1", e.Live())
	}
	roots := e.Roots()
	if len(roots) != 1 || roots[0] != 21 {
		t.Fatalf("roots = %v, want [21] (orphan becomes a root)", roots)
	}
}

func TestReapUnknownProcessIsHarmless(t *testing.T) {
	e := testEngine(t, time.Minute)
	e.Reap(9999)
}

func TestWindowSnapshotMatchesAggregate(t *testing.T) {
	e := testEngine(t, time.Minute)
	now := time.Now()
	e.Apply(event.Event{Kind: event.KindProcessStart, PID: 3, ProcName: "solo", Time: now})
	e.Apply(write(3, "/data/a.bin", 42, now))

	win, ok := e.Window(3)
	if !ok {
		t.Fatal("window not found for a live process")
	}
	tv := e.Aggregate(3)

	if win.Writes != tv.Writes || win.Bytes != tv.Bytes {
		t.Fatalf("snapshot (%d writes, %d bytes) disagrees with aggregate (%d, %d)",
			win.Writes, win.Bytes, tv.Writes, tv.Bytes)
	}
}

func TestWindowMissingProcessReportsNotOK(t *testing.T) {
	e := testEngine(t, time.Minute)
	if _, ok := e.Window(1234); ok {
		t.Fatal("expected not-ok for an unknown process")
	}
}
