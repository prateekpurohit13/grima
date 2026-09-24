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

func signalNamed(signals []score.Signal, name string) (score.Signal, bool) {
	for _, sg := range signals {
		if sg.Name == name {
			return sg, true
		}
	}
	return score.Signal{}, false
}

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

// Characterisation of what the n-gram signal fires on, using the event shapes
// measured on this host with the real sensor (see the report's harness runs):
//
//	atomic save over an existing file   create, write, delete, rename
//	extract / install / release script  create, write, rename
//	encryptor (write in place, rename)  write, rename, create
//
// An atomic save is silent: replacing an existing target makes the sensor report
// the temp file's removal as a delete between the write and the rename, so no
// write>rename adjacency exists anywhere in the window.
//
// An extraction is not silent, and the reason matters: its cycle is a rotation
// of the encryptor's cycle, so the k-grams of the two are the same multiset and
// this feature cannot separate them. The evidence that does separate them is
// content-derived (entropy, magic bytes, extension novelty), which is why the
// signal is Secondary and ships at a weight that cannot alert on its own
// (0.2 — a benign extraction reaches the low band, not the medium one).
func TestNGramRenameChainOnAtomicSaveAndExtractionShapes(t *testing.T) {
	cfg := config.Default() // 30s window, so 40 writes is 1.3/s
	scorer := score.NewScorer(cfg)

	measure := func(procName string, cycle []event.Kind, reps int) score.Verdict {
		engine := fingerprint.NewEngine(cfg)
		now := time.Now()
		engine.Apply(event.Event{Kind: event.KindProcessStart, PID: 21, ProcName: procName, Time: now})
		for range reps {
			for _, kind := range cycle {
				engine.Apply(event.Event{Kind: kind, PID: 21, Path: "/data/f.docx", Bytes: 4096, Time: now})
			}
		}
		return scorer.Evaluate(score.Inputs{Tree: engine.Aggregate(21)})
	}

	atomicSave := measure("atomic-save", []event.Kind{
		event.KindFileCreate, event.KindFileWrite, event.KindFileDelete, event.KindFileRename,
	}, 40)
	if sg, ok := signalNamed(atomicSave.Signals, "ngram_rename_chain"); ok {
		t.Fatalf("an atomic save burst fired the n-gram signal: %s", sg.Detail)
	}

	extraction := measure("extractor", []event.Kind{
		event.KindFileCreate, event.KindFileWrite, event.KindFileRename,
	}, 40)
	encryption := measure("encryptor", []event.Kind{
		event.KindFileWrite, event.KindFileRename, event.KindFileCreate,
	}, 40)

	for name, v := range map[string]score.Verdict{"extraction": extraction, "encryption": encryption} {
		sg, ok := signalNamed(v.Signals, "ngram_rename_chain")
		if !ok {
			t.Fatalf("%s did not fire the n-gram signal: %v", name, v.Signals)
		}
		if sg.Value != 1 {
			t.Fatalf("%s fired at value %.2f, want the saturated 1", name, sg.Value)
		}
		t.Logf("%s: score %.1f level %v — %s", name, v.Score, v.Level, sg.Detail)
	}
	if extraction.Score != encryption.Score {
		t.Fatalf("extraction scored %.1f and encryption %.1f: the shapes are rotations of one cycle, so the signal must treat them alike",
			extraction.Score, encryption.Score)
	}
	// The shipped weight exists to keep this signal from alerting on its own: an
	// extraction is a benign workload, so alone it must stay under the medium
	// band. That is not the same as making fusion safe — measured separately, it
	// still lifts a companion that would sit at 33 up to 47 — but raising the
	// weight has to be a deliberate act, not a drift.
	if medium := cfg.Scoring.LevelBands.Medium; extraction.Score >= medium {
		t.Fatalf("a benign extraction reached %.1f with the n-gram signal alone, at or above the medium band %.0f",
			extraction.Score, medium)
	}
}
