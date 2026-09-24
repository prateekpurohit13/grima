package calibrate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleBaseline() *Baseline {
	return &Baseline{
		Version:         Version,
		Host:            "testhost",
		CapturedAt:      time.Now().Truncate(time.Second),
		WarmupSeconds:   600,
		MinSamples:      2,
		SigmaFloor:      0.05,
		EntropyByExt:    map[string]Dist{".docx": {Mean: 4.2, StdDev: 0.3, N: 50}},
		WriteRateByProc: map[string]float64{"code": 12.5},
		KnownExt:        []string{".docx", ".txt"},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")

	if err := sampleBaseline().Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("baseline did not load")
	}
	if loaded.Host != "testhost" {
		t.Fatalf("host = %q", loaded.Host)
	}
	if got := loaded.EntropyByExt[".docx"].Mean; got != 4.2 {
		t.Fatalf("mean = %v, want 4.2", got)
	}
	if got := loaded.WriteRateByProc["code"]; got != 12.5 {
		t.Fatalf("write rate = %v, want 12.5", got)
	}
	if !loaded.Ready() {
		t.Fatal("a baseline with samples should be ready")
	}
}

// Sad path: a baseline from a different schema version is ignored, not fatal.
func TestLoadIgnoresVersionMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	body := `{"version": 999, "min_samples": 1, "entropy_by_ext": {".txt": {"mean":1,"std_dev":0.1,"n":10}}}`

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("version mismatch must not be an error: %v", err)
	}
	if loaded != nil {
		t.Fatal("expected a version mismatch to yield nil")
	}
}

// Sad path: a missing baseline means uncalibrated mode, not a failure to start.
func TestLoadMissingFileReturnsNil(t *testing.T) {
	loaded, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("missing file must not be an error: %v", err)
	}
	if loaded != nil {
		t.Fatal("expected nil for a missing baseline")
	}
}

// Sad path: a corrupt baseline is ignored rather than crashing the detector.
func TestLoadCorruptFileReturnsNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("corrupt file must not be an error: %v", err)
	}
	if loaded != nil {
		t.Fatal("expected nil for a corrupt baseline")
	}
}

func TestLoadEmptyPathReturnsNil(t *testing.T) {
	loaded, err := Load("")
	if err != nil || loaded != nil {
		t.Fatalf("Load(\"\") = (%v, %v), want (nil, nil)", loaded, err)
	}
}

func TestSaveRejectsEmptyPath(t *testing.T) {
	if err := sampleBaseline().Save(""); err == nil {
		t.Fatal("saving to an empty path should fail")
	}
}

func TestReadyRequiresSamples(t *testing.T) {
	baseline := sampleBaseline()
	baseline.MinSamples = 1000
	if baseline.Ready() {
		t.Fatal("baseline should not be ready with too few samples")
	}

	var absent *Baseline
	if absent.Ready() {
		t.Fatal("a nil baseline is never ready")
	}
}

func TestSigmaAppliesFloor(t *testing.T) {
	baseline := sampleBaseline()
	baseline.EntropyByExt[".flat"] = Dist{Mean: 7.9, StdDev: 0, N: 10}

	dist, ok := baseline.Sigma(".flat")
	if !ok {
		t.Fatal("expected a usable distribution")
	}
	if dist.StdDev != baseline.SigmaFloor {
		t.Fatalf("stddev = %v, want the floor %v", dist.StdDev, baseline.SigmaFloor)
	}
}

func TestSigmaRejectsUnknownExtension(t *testing.T) {
	if _, ok := sampleBaseline().Sigma(".nope"); ok {
		t.Fatal("an extension with no samples has no usable distribution")
	}
}

func TestKnowsExt(t *testing.T) {
	baseline := sampleBaseline()

	if !baseline.KnowsExt(".docx") {
		t.Fatal(".docx is known")
	}
	if baseline.KnowsExt(".locked") {
		t.Fatal(".locked should be unknown")
	}
	if !baseline.KnowsExt("") {
		t.Fatal("an empty extension is not evidence of anything")
	}

	var absent *Baseline
	if !absent.KnowsExt(".locked") {
		t.Fatal("with no baseline, nothing can be called unknown")
	}
}
