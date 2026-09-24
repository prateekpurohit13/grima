package decoy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prateekpurohit13/grima/internal/config"
)

func plantConfig(dir string) config.Config {
	cfg := config.Default()
	cfg.General.MonitorPaths = []string{dir}
	cfg.Decoy.Enabled = true
	cfg.Decoy.Names = []string{"_grima_canary.doc", "quarterly_report_2019.xlsx", "invoices_backup.pdf"}
	cfg.Decoy.CountPerDir = 2
	return cfg
}

func TestPlantWritesAndRegistersDecoys(t *testing.T) {
	dir := t.TempDir()
	cfg := plantConfig(dir)
	reg := NewRegistry()

	planted, err := Plant(cfg, reg)
	if err != nil {
		t.Fatalf("Plant: %v", err)
	}
	if planted != cfg.Decoy.CountPerDir {
		t.Fatalf("planted = %d, want %d", planted, cfg.Decoy.CountPerDir)
	}
	if reg.Count() != cfg.Decoy.CountPerDir {
		t.Fatalf("registered = %d, want %d", reg.Count(), cfg.Decoy.CountPerDir)
	}

	for i := 0; i < cfg.Decoy.CountPerDir; i++ {
		path := filepath.Join(dir, cfg.Decoy.Names[i])

		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("decoy file %s missing: %v", path, err)
		}
		if string(body) != decoyBody {
			t.Fatalf("decoy body = %q, want %q", body, decoyBody)
		}

		id, ok := reg.Lookup(path)
		if !ok {
			t.Fatalf("Lookup(%s) says not a decoy", path)
		}
		if id == "" {
			t.Fatalf("decoy id for %s is empty", path)
		}
	}

	// The name past count_per_dir must not be planted.
	beyond := filepath.Join(dir, cfg.Decoy.Names[cfg.Decoy.CountPerDir])
	if _, err := os.Stat(beyond); err == nil {
		t.Fatalf("%s was planted beyond count_per_dir", beyond)
	}
	if id, ok := reg.Lookup(beyond); ok {
		t.Fatalf("Lookup(%s) = %q, want not a decoy", beyond, id)
	}
}

func TestLookupRejectsPathsThatAreNotDecoys(t *testing.T) {
	dir := t.TempDir()
	cfg := plantConfig(dir)
	reg := NewRegistry()
	if _, err := Plant(cfg, reg); err != nil {
		t.Fatalf("Plant: %v", err)
	}

	plain := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(plain, []byte("mine"), 0o644); err != nil {
		t.Fatalf("write %s: %v", plain, err)
	}

	if id, ok := reg.Lookup(plain); ok {
		t.Fatalf("Lookup(%s) = %q, want not a decoy", plain, id)
	}
	if id, ok := reg.Lookup(filepath.Join(dir, "does-not-exist.doc")); ok {
		t.Fatalf("Lookup of a missing file = %q, want not a decoy", id)
	}
}

// A registry keyed by path must match the same file however the path is spelled.
// filepath.Join cleans, so the unclean spelling is built by concatenation —
// otherwise the test would only ever exercise clean paths on both sides.
func TestLookupIgnoresPathSpelling(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()

	sep := string(filepath.Separator)
	clean := filepath.Join(dir, "_grima_canary.doc")
	unclean := filepath.Join(dir, "sub") + sep + ".." + sep + "_grima_canary.doc"
	if unclean == clean {
		t.Fatalf("fixture is not actually unclean: %q", unclean)
	}

	reg.Add(unclean, "canary-1")

	if id, ok := reg.Lookup(clean); !ok || id != "canary-1" {
		t.Fatalf("Lookup(%s) = %q, %v; want canary-1, true", clean, id, ok)
	}
	if id, ok := reg.Lookup(unclean); !ok || id != "canary-1" {
		t.Fatalf("Lookup(%s) = %q, %v; want canary-1, true", unclean, id, ok)
	}
}

func TestPlantTwiceKeepsOneEntryPerDecoy(t *testing.T) {
	dir := t.TempDir()
	cfg := plantConfig(dir)
	reg := NewRegistry()

	if _, err := Plant(cfg, reg); err != nil {
		t.Fatalf("first Plant: %v", err)
	}
	first, _ := reg.Lookup(filepath.Join(dir, cfg.Decoy.Names[0]))

	if _, err := Plant(cfg, reg); err != nil {
		t.Fatalf("second Plant: %v", err)
	}
	if reg.Count() != cfg.Decoy.CountPerDir {
		t.Fatalf("registered = %d after planting twice, want %d", reg.Count(), cfg.Decoy.CountPerDir)
	}
	second, ok := reg.Lookup(filepath.Join(dir, cfg.Decoy.Names[0]))
	if !ok || second != first {
		t.Fatalf("decoy id changed between plants: %q then %q", first, second)
	}
}

func TestPlantKeepsAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	cfg := plantConfig(dir)

	existing := filepath.Join(dir, cfg.Decoy.Names[0])
	const operatorContent = "the operator's own file"
	if err := os.WriteFile(existing, []byte(operatorContent), 0o644); err != nil {
		t.Fatalf("write %s: %v", existing, err)
	}

	reg := NewRegistry()
	planted, err := Plant(cfg, reg)
	if err != nil {
		t.Fatalf("Plant: %v", err)
	}
	if planted != cfg.Decoy.CountPerDir {
		t.Fatalf("planted = %d, want %d", planted, cfg.Decoy.CountPerDir)
	}

	body, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read %s: %v", existing, err)
	}
	if string(body) != operatorContent {
		t.Fatalf("existing file was overwritten: %q", body)
	}
	if _, ok := reg.Lookup(existing); !ok {
		t.Fatalf("Lookup(%s) says not a decoy", existing)
	}
}

func TestPlantDisabledPlantsNothing(t *testing.T) {
	dir := t.TempDir()
	cfg := plantConfig(dir)
	cfg.Decoy.Enabled = false

	reg := NewRegistry()
	planted, err := Plant(cfg, reg)
	if err != nil {
		t.Fatalf("Plant: %v", err)
	}
	if planted != 0 || reg.Count() != 0 {
		t.Fatalf("planted = %d, registered = %d; want 0 and 0", planted, reg.Count())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Fatalf("%d files created with decoys disabled", len(entries))
	}
}

func TestPlantSkipsAnUnreadableRootAndPlantsTheRest(t *testing.T) {
	dir := t.TempDir()
	cfg := plantConfig(dir)
	missing := filepath.Join(dir, "missing")
	cfg.General.MonitorPaths = []string{missing, dir}

	reg := NewRegistry()
	planted, err := Plant(cfg, reg)
	if err == nil {
		t.Fatalf("Plant reported no error for the unreadable root %s", missing)
	}
	if planted != cfg.Decoy.CountPerDir {
		t.Fatalf("planted = %d, want %d from the readable root", planted, cfg.Decoy.CountPerDir)
	}
	if reg.Count() != cfg.Decoy.CountPerDir {
		t.Fatalf("registered = %d, want %d", reg.Count(), cfg.Decoy.CountPerDir)
	}
}
