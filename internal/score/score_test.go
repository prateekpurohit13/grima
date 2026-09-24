package score

import (
	"testing"
	"time"

	"github.com/prateekpurohit13/grima/internal/calibrate"
	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/fingerprint"
)

func testBaseline() *calibrate.Baseline {
	return &calibrate.Baseline{
		Version:    calibrate.Version,
		MinSamples: 1,
		SigmaFloor: 0.05,
		EntropyByExt: map[string]calibrate.Dist{
			".docx": {Mean: 4.0, StdDev: 0.5, N: 100},
		},
		WriteRateByProc: map[string]float64{},
		HostEventRate:   calibrate.Dist{Mean: 5, StdDev: 1, N: 100},
		DirFanout:       calibrate.Dist{Mean: 2, StdDev: 1, N: 100},
		KnownExt:        []string{".docx", ".txt"},
	}
}

func encryptedTree() fingerprint.TreeVector {
	return fingerprint.TreeVector{
		Root:           1,
		ProcName:       "cryptor",
		PIDs:           []int32{1},
		Writes:         500,
		Renames:        200,
		WindowDuration: time.Second,
		Entropy:        []fingerprint.EntropySample{{Ext: ".docx", H: 7.9}},
		MagicTotal:     500,
		MagicMismatch:  500,
		Dirs:           map[string]struct{}{"/data/a": {}, "/data/b": {}},
		ExtActivity:    map[string]int64{".locked": 500},
	}
}

func TestEncryptionLikeActivityScoresHigh(t *testing.T) {
	scorer := NewScorer(config.Default())

	verdict := scorer.Evaluate(Inputs{Tree: encryptedTree(), Baseline: testBaseline()})

	if verdict.Level < LevelHigh {
		t.Fatalf("level = %v, want at least high (score %.1f)", verdict.Level, verdict.Score)
	}
	if len(verdict.Signals) == 0 {
		t.Fatal("a high verdict must carry evidence")
	}

	names := map[string]bool{}
	for _, sg := range verdict.Signals {
		names[sg.Name] = true
		if sg.Detail == "" {
			t.Fatalf("signal %q has no detail", sg.Name)
		}
	}
	for _, want := range []string{"entropy_deviation", "magic_mismatch", "write_burst", "unknown_extension_activity"} {
		if !names[want] {
			t.Fatalf("missing expected signal %q; got %v", want, names)
		}
	}
}

func TestQuietActivityScoresLow(t *testing.T) {
	scorer := NewScorer(config.Default())

	verdict := scorer.Evaluate(Inputs{
		Tree:     fingerprint.TreeVector{Root: 2, ProcName: "editor", WindowDuration: time.Second},
		Baseline: testBaseline(),
	})

	if verdict.Level > LevelLow {
		t.Fatalf("level = %v, want low or below for quiet activity", verdict.Level)
	}
}

// Sad path: with no baseline, deviation signals are unknown, not zero.
func TestUncalibratedOmitsDeviationSignals(t *testing.T) {
	scorer := NewScorer(config.Default())

	verdict := scorer.Evaluate(Inputs{Tree: encryptedTree(), Baseline: nil})

	if verdict.Calibrated {
		t.Fatal("verdict claims to be calibrated with no baseline")
	}
	for _, sg := range verdict.Signals {
		switch sg.Name {
		case "entropy_deviation", "write_burst", "rename_burst", "unknown_extension_activity", "dir_fanout", "delete_rate":
			t.Fatalf("deviation signal %q present while uncalibrated", sg.Name)
		}
	}
}

// Sad path: an unready baseline is treated as absent.
func TestUnreadyBaselineIsNotCalibrated(t *testing.T) {
	scorer := NewScorer(config.Default())
	baseline := testBaseline()
	baseline.MinSamples = 10_000

	verdict := scorer.Evaluate(Inputs{Tree: encryptedTree(), Baseline: baseline})
	if verdict.Calibrated {
		t.Fatal("a baseline without enough samples must not count as calibrated")
	}
}

func TestOverrideSetsFloorAndIsNotAveragedAway(t *testing.T) {
	scorer := NewScorer(config.Default())

	verdict := scorer.Evaluate(Inputs{
		Tree:     fingerprint.TreeVector{Root: 3, WindowDuration: time.Second},
		Baseline: testBaseline(),
		Overrides: []Override{{
			ID:     "R-DECOY-TOUCH",
			Level:  LevelCritical,
			Detail: "decoy touched",
		}},
	})

	if verdict.Level != LevelCritical {
		t.Fatalf("level = %v, want critical from the override floor", verdict.Level)
	}
	if verdict.Override != "R-DECOY-TOUCH" {
		t.Fatalf("override = %q, want R-DECOY-TOUCH", verdict.Override)
	}
}

func TestSigmaFloorPreventsDivideByZero(t *testing.T) {
	baseline := testBaseline()
	baseline.EntropyByExt[".dat"] = calibrate.Dist{Mean: 7.9, StdDev: 0, N: 50}

	dist, ok := baseline.Sigma(".dat")
	if !ok {
		t.Fatal("expected a usable distribution")
	}
	if dist.StdDev <= 0 {
		t.Fatalf("stddev = %v, want the configured floor applied", dist.StdDev)
	}
}

func TestSigmaRejectsThinDistributions(t *testing.T) {
	baseline := testBaseline()
	baseline.EntropyByExt[".thin"] = calibrate.Dist{Mean: 1, StdDev: 1, N: 1}

	if _, ok := baseline.Sigma(".thin"); ok {
		t.Fatal("a single-sample distribution is not usable")
	}
}

func TestParseLevelRoundTrip(t *testing.T) {
	for _, name := range []string{"info", "low", "medium", "high", "critical"} {
		lvl, ok := ParseLevel(name)
		if !ok || lvl.String() != name {
			t.Fatalf("ParseLevel(%q) = (%v, %v)", name, lvl, ok)
		}
	}
	if _, ok := ParseLevel("apocalyptic"); ok {
		t.Fatal("unknown level name should not parse")
	}
}

func TestKnowsExtTreatsUnknownAsUnknown(t *testing.T) {
	baseline := testBaseline()
	if !baseline.KnowsExt(".docx") {
		t.Fatal(".docx is in the known set")
	}
	if baseline.KnowsExt(".locked") {
		t.Fatal(".locked is not in the known set")
	}
	if !baseline.KnowsExt("") {
		t.Fatal("an empty extension should not be reported as unknown")
	}
}

// Adding evidence must never lower the score.
//
// A weighted mean fails this, and that is not hypothetical: the same encryption
// burst scored 100 (critical) on a local run where one signal fired, and 56.4
// (medium) in CI where four fired, because the saturated signal was averaged
// against three weaker ones. More evidence of the same attack read as less
// severe.
func TestAddingEvidenceNeverLowersTheScore(t *testing.T) {
	scorer := NewScorer(config.Default())
	baseline := testBaseline()

	base := fingerprint.TreeVector{
		Root:           1,
		ProcName:       "cryptor",
		WindowDuration: time.Second,
		ExtActivity:    map[string]int64{".locked": 60},
	}
	more := base
	more.Writes = 500
	more.Entropy = []fingerprint.EntropySample{{Ext: ".docx", H: 7.9}}
	more.MagicTotal = 500
	more.MagicMismatch = 500

	fewer := scorer.Evaluate(Inputs{Tree: base, Baseline: baseline})
	extra := scorer.Evaluate(Inputs{Tree: more, Baseline: baseline})

	if len(extra.Signals) <= len(fewer.Signals) {
		t.Fatalf("the second verdict should carry more signals: %d then %d",
			len(fewer.Signals), len(extra.Signals))
	}
	if extra.Score < fewer.Score {
		t.Fatalf("more evidence scored lower: %.1f with %d signals, %.1f with %d",
			fewer.Score, len(fewer.Signals), extra.Score, len(extra.Signals))
	}
}
