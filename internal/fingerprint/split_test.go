// This test drives the engine from the outside on purpose: it is the tree
// aggregation claim, not an engine internal, that is being pinned.
package fingerprint_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
	"github.com/prateekpurohit13/grima/internal/fingerprint"
	"github.com/prateekpurohit13/grima/internal/score"
)

// 2.6: a workload that splits encryption across cooperating children keeps every
// child below the threshold that would alert on it alone. The tree aggregate is
// what turns N quiet processes into one loud actor.
//
// Synthetic events only — no workloads, no sleeps — so the result is about the
// aggregation, not about timing.
func TestSplitWorkloadScoresAsOneActorAboveThreshold(t *testing.T) {
	cfg := config.Default()
	// A ten-second window turns the rate into a count: absolute_write_rate is
	// 20 writes/s, so the threshold is 200 writes.
	cfg.Window.DecayHalfLife = config.Duration(10 * time.Second)
	scorer := score.NewScorer(cfg)
	engine := fingerprint.NewEngine(cfg)
	now := time.Now()

	const (
		parent      = int32(4000)
		workers     = 5
		rootWrites  = 5
		childWrites = 80
	)
	threshold := cfg.Scoring.AbsoluteWriteRate * cfg.Window.DecayHalfLife.Std().Seconds()
	if float64(rootWrites) >= threshold || float64(childWrites) >= threshold {
		t.Fatalf("test premise broken: %d root and %d child writes must each be below the %.0f-write threshold",
			rootWrites, childWrites, threshold)
	}

	writeFile := func(pid int32, path string) event.Event {
		return event.Event{Kind: event.KindFileWrite, PID: pid, Path: path, Bytes: 4096, Time: now}
	}

	engine.Apply(event.Event{Kind: event.KindProcessStart, PID: parent, ProcName: "splitter", Time: now})
	for i := range rootWrites {
		engine.Apply(writeFile(parent, fmt.Sprintf("/data/root%d.bin", i)))
	}
	for w := range workers {
		pid := parent + 1 + int32(w)
		engine.Apply(event.Event{
			Kind: event.KindProcessStart, PID: pid, PPID: parent, ProcName: "cryptor", Time: now,
		})
		for i := range childWrites {
			engine.Apply(writeFile(pid, fmt.Sprintf("/data/part%d_%d.bin", w, i)))
		}
	}

	// Each child, scored on its own, is quiet: no signal fires, so nothing is
	// reported. That is what "below threshold" has to mean in observable terms.
	for w := range workers {
		pid := parent + 1 + int32(w)
		own := engine.Aggregate(pid)
		if own.Writes != childWrites {
			t.Fatalf("child %d aggregate = %d writes, want its own %d", pid, own.Writes, childWrites)
		}
		if v := scorer.Evaluate(score.Inputs{Tree: own}); len(v.Signals) != 0 {
			t.Fatalf("child %d alerted on its own: %v", pid, v.Signals)
		}
	}

	tree := engine.Aggregate(parent)
	want := rootWrites + workers*childWrites
	if tree.Writes != want {
		t.Fatalf("tree writes = %d, want %d from the root and all %d workers", tree.Writes, want, workers)
	}
	if len(tree.PIDs) != workers+1 {
		t.Fatalf("tree pids = %v, want the root and all %d workers", tree.PIDs, workers)
	}
	if float64(tree.Writes) <= threshold {
		t.Fatalf("tree writes = %d, want more than the %.0f-write threshold", tree.Writes, threshold)
	}

	verdict := scorer.Evaluate(score.Inputs{Tree: tree})
	var detail string
	for _, sg := range verdict.Signals {
		if sg.Name == "write_rate_absolute" {
			detail = sg.Detail
		}
	}
	if detail == "" {
		t.Fatalf("the split tree produced no rate signal; signals = %v", verdict.Signals)
	}
	if verdict.Score <= 0 {
		t.Fatalf("the split tree scored %.1f, want above zero", verdict.Score)
	}
	t.Logf("split tree: %d writes across %d processes, score %.1f, %s",
		tree.Writes, len(tree.PIDs), verdict.Score, detail)
}
