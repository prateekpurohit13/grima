package calibrate_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prateekpurohit13/grima/internal/calibrate"
	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
	"github.com/prateekpurohit13/grima/internal/fingerprint"
	"github.com/prateekpurohit13/grima/internal/score"
)

// The recalibration feedback edge, asserted through the scorer rather than
// through the baseline's fields: a workload the operator has confirmed benign
// must stop alerting once its activity is part of the baseline.
//
// The workload is a build writing object files — nothing is wrong with it, and
// nothing on this host has seen it before, which is exactly the case that
// alerts. The only difference between the two verdicts is the recalibration, so
// a regression in the merge shows up as the second verdict failing to fall.
func TestRecalibrationStopsAlertingForAConfirmedWorkload(t *testing.T) {
	cfg := config.Default()
	cfg.General.MonitorPaths = nil // measure the events only, not this machine's disk
	cfg.Calibration.MinSamples = 20

	start := time.Unix(1_700_000_000, 0)

	// The host as measured: ordinary document writes, one event a second.
	quiet := calibrate.NewObservations()
	for i := range 60 {
		quiet.Observe(write(start.Add(time.Duration(i)*time.Second), "explorer.exe",
			`C:\docs\report.txt`, 4.0, 100, 11))
	}
	baseline := quiet.Baseline(cfg, 60*time.Second)
	if !baseline.Ready() {
		t.Fatal("the baseline is too thin to score against")
	}

	// The workload: one process writing a new extension, 200 files over 30
	// seconds, spread across 40 directories.
	build := calibrate.NewObservations()
	for i := range 200 {
		build.Observe(write(start.Add(time.Duration(i)*150*time.Millisecond), "cc1.exe",
			filepath.Join(`C:\src`, fmt.Sprintf("pkg%02d", i%40), fmt.Sprintf("unit_%03d.o", i)),
			7.2, 100, 4242))
	}

	tree := fingerprint.TreeVector{
		Root:           4242,
		ProcName:       "cc1.exe",
		PIDs:           []int32{4242},
		Writes:         200,
		Bytes:          200 * 4096,
		Dirs:           dirs(40),
		Entropy:        entropySamples(".o", 7.2, 200),
		ExtActivity:    map[string]int64{".o": 200},
		WindowDuration: 30 * time.Second,
	}

	scorer := score.NewScorer(cfg)
	before := scorer.Evaluate(score.Inputs{Tree: tree, Baseline: baseline})
	if !before.Calibrated {
		t.Fatal("the verdict was not scored against the baseline")
	}
	t.Logf("before recalibration: score %.1f level %s signals %s", before.Score, before.Level, signalNames(before))
	if before.Level < score.LevelMedium {
		t.Fatalf("the workload did not alert before recalibration: score %.1f level %s signals %s",
			before.Score, before.Level, signalNames(before))
	}

	// The operator confirms the workload and recalibrates while it runs.
	if _, err := baseline.Merge(build.Baseline(cfg, 30*time.Second)); err != nil {
		t.Fatalf("recalibrate: %v", err)
	}

	after := scorer.Evaluate(score.Inputs{Tree: tree, Baseline: baseline})
	t.Logf("after recalibration:  score %.1f level %s signals %s", after.Score, after.Level, signalNames(after))
	if after.Level >= score.LevelMedium {
		t.Fatalf("the confirmed workload still alerts after recalibration: score %.1f level %s signals %s",
			after.Score, after.Level, signalNames(after))
	}
	if after.Score >= before.Score {
		t.Fatalf("recalibration did not lower the score: %.1f -> %.1f", before.Score, after.Score)
	}

	// What made it alert must be gone, not merely outweighed by weaker
	// companions.
	//
	// `unknown_extension_activity` is fully absorbed: the extension is promoted
	// to known, so the signal disappears outright.
	//
	// `write_burst` is not, and that is expected rather than a regression. It is
	// measured against the host write rate — a per-process baseline is only
	// meaningful when attribution is causal, and under correlative attribution
	// the blamed process is a guess, which is what produced the atomic-save
	// false positive. Merging pools distributions, so one recalibration lowers a
	// large rate deviation sharply (0.81 -> 0.19 here) without erasing it.
	// Repeated recalibration converges; a single pass does not.
	for _, sg := range after.Signals {
		if sg.Name == "unknown_extension_activity" {
			t.Fatalf("recalibration left %s (%.2f) in the verdict", sg.Name, sg.Value)
		}
	}

	// The workload's rate must at least be substantially absorbed.
	for _, sg := range after.Signals {
		if sg.Name == "write_burst" && sg.Value >= 0.5 {
			t.Fatalf("recalibration barely moved write_burst: %.2f", sg.Value)
		}
	}
}

func write(at time.Time, proc, path string, entropy float64, pid int32, ppid int32) event.Event {
	return event.Event{
		Time:     at,
		Kind:     event.KindFileWrite,
		PID:      pid,
		PPID:     ppid,
		ProcName: proc,
		Path:     path,
		Entropy:  entropy,
		Bytes:    4096,
	}
}

func entropySamples(ext string, h float64, n int) []fingerprint.EntropySample {
	out := make([]fingerprint.EntropySample, n)
	for i := range out {
		out[i] = fingerprint.EntropySample{Ext: ext, H: h}
	}
	return out
}

func dirs(n int) map[string]struct{} {
	out := make(map[string]struct{}, n)
	for i := range n {
		out[filepath.Join(`C:\src`, fmt.Sprintf("pkg%02d", i))] = struct{}{}
	}
	return out
}

func signalNames(v score.Verdict) string {
	names := make([]string, 0, len(v.Signals))
	for _, sg := range v.Signals {
		names = append(names, fmt.Sprintf("%s=%.2f", sg.Name, sg.Value))
	}
	return strings.Join(names, " ")
}
