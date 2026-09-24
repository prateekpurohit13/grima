//go:build linux

package persistwatch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
)

// pointScannerAt replaces this host's persistence locations with the given ones
// for the duration of the test.
func pointScannerAt(t *testing.T, cron, units, files []string) {
	t.Helper()

	oldCron, oldUnits, oldFiles := cronDirs, unitDirs, cronFiles
	cronDirs, unitDirs, cronFiles = cron, units, files
	t.Cleanup(func() {
		cronDirs, unitDirs, cronFiles = oldCron, oldUnits, oldFiles
	})
}

// Happy path: a real Linux host has readable cron or systemd directories, so the
// sensor starts observing rather than reporting itself blind.
func TestScanPersistenceReadsThisHost(t *testing.T) {
	entries, err := scanPersistence()
	if err != nil {
		// A host with neither cron nor systemd is not a scanner failure.
		t.Skipf("this host has no readable persistence location: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no cron or systemd entries found on this host")
	}
	for _, e := range entries {
		if e.path == "" || e.kind == "" {
			t.Fatalf("entry is missing its path or kind: %+v", e)
		}
	}
}

// Happy path with known contents: cron files and unit files are returned with
// the right kind, and subdirectories are not persistence entries.
func TestScanPersistenceFindsCronAndSystemdEntries(t *testing.T) {
	cron := t.TempDir()
	writeFile(t, filepath.Join(cron, "grima-cron-entry"))
	if err := os.Mkdir(filepath.Join(cron, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	units := t.TempDir()
	writeFile(t, filepath.Join(units, "grima-test.service"))

	file := filepath.Join(t.TempDir(), "crontab")
	writeFile(t, file)

	pointScannerAt(t, []string{cron}, []string{units}, []string{file})

	entries, err := scanPersistence()
	if err != nil {
		t.Fatalf("scanPersistence: %v", err)
	}

	want := map[string]string{
		filepath.Join(cron, "grima-cron-entry"):    "cron",
		filepath.Join(units, "grima-test.service"): "systemd",
		file: "cron",
	}
	if len(entries) != len(want) {
		t.Fatalf("entries = %+v, want %d entries", entries, len(want))
	}
	for _, e := range entries {
		if want[e.path] != e.kind {
			t.Errorf("entry %q has kind %q, want %q", e.path, e.kind, want[e.path])
		}
	}
}

// Sad path: when not one location can be read the scan must fail, because the
// sensor reports "cannot observe" rather than starting blind.
func TestScanPersistenceFailsWhenNothingIsReadable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	pointScannerAt(t, []string{missing}, []string{missing}, []string{missing})

	entries, err := scanPersistence()
	if err == nil {
		t.Fatalf("scanPersistence returned %d entries and no error; a blind scan must fail", len(entries))
	}
	if entries != nil {
		t.Fatalf("entries = %+v, want none alongside the error", entries)
	}
}

// The entries present before Start are the baseline and never alert; a cron
// entry created afterwards does.
func TestSourceAlertsOnCronEntryAddedAfterStart(t *testing.T) {
	cron := t.TempDir()
	writeFile(t, filepath.Join(cron, "existing-cron-entry"))
	pointScannerAt(t, []string{cron}, nil, nil)

	src := New(config.Default(), quietLogger())
	out := make(chan event.Event, 8)

	if err := src.Start(context.Background(), out); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer src.Close()

	src.poll(out)
	if len(out) != 0 {
		t.Fatalf("a pre-existing cron entry raised an alert: %+v", <-out)
	}

	newPath := filepath.Join(cron, "grima-sprint1-test")
	writeFile(t, newPath)
	src.poll(out)

	select {
	case ev := <-out:
		if ev.Kind != event.KindPersistenceInstall {
			t.Errorf("kind = %v, want %v", ev.Kind, event.KindPersistenceInstall)
		}
		if ev.Path != newPath {
			t.Errorf("path = %q, want %q", ev.Path, newPath)
		}
		if ev.Persist != "cron" {
			t.Errorf("persist = %q, want cron", ev.Persist)
		}
	default:
		t.Fatal("no event for a cron entry created after Start")
	}
}
