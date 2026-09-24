//go:build windows

package persistwatch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
	"golang.org/x/sys/windows/registry"
)

// pointScannerAt replaces this host's persistence locations with the given ones
// for the duration of the test.
func pointScannerAt(t *testing.T, keys []runKey, dirs []string, taskDir string) {
	t.Helper()

	oldKeys, oldDirs, oldTasks := runKeys, startupDirs, tasksDir
	runKeys, startupDirs, tasksDir = keys, dirs, taskDir
	t.Cleanup(func() {
		runKeys, startupDirs, tasksDir = oldKeys, oldDirs, oldTasks
	})
}

// Happy path: a real Windows host has readable run keys and Startup folders, so
// the sensor starts observing rather than reporting itself blind.
func TestScanPersistenceReadsThisHost(t *testing.T) {
	entries, err := scanPersistence()
	if err != nil {
		t.Fatalf("scanPersistence on this host: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no persistence entries found on a host with a logged-in user")
	}
	for _, e := range entries {
		if e.path == "" || e.kind == "" {
			t.Fatalf("entry is missing its path or kind: %+v", e)
		}
	}
}

// Happy path with known contents: startup files and task files are returned with
// the right kind, and subdirectories are not persistence entries.
func TestScanPersistenceFindsStartupAndTaskFiles(t *testing.T) {
	startup := t.TempDir()
	writeFile(t, filepath.Join(startup, "new-startup.lnk"))
	if err := os.Mkdir(filepath.Join(startup, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	tasks := t.TempDir()
	writeFile(t, filepath.Join(tasks, "NewTask"))

	pointScannerAt(t, nil, []string{startup}, tasks)

	entries, err := scanPersistence()
	if err != nil {
		t.Fatalf("scanPersistence: %v", err)
	}

	want := map[string]string{
		filepath.Join(startup, "new-startup.lnk"): "startup",
		filepath.Join(tasks, "NewTask"):           "task",
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
	pointScannerAt(t, nil, []string{missing}, missing)

	entries, err := scanPersistence()
	if err == nil {
		t.Fatalf("scanPersistence returned %d entries and no error; a blind scan must fail", len(entries))
	}
	if entries != nil {
		t.Fatalf("entries = %+v, want none alongside the error", entries)
	}
}

// Happy path: the current user's Run key exists on any host with a logon.
func TestReadRunKeyReadsThisHostsRunKey(t *testing.T) {
	if _, ok := readRunKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`); !ok {
		t.Fatal("the current user's Run key must be readable")
	}
}

// The run-key path is exercised against the real registry: every value in the
// current user's Run key becomes an entry named after the key and the value.
func TestScanPersistenceNamesRunKeyEntriesAfterKeyAndValue(t *testing.T) {
	key := `Software\Microsoft\Windows\CurrentVersion\Run`
	values, ok := readRunKey(registry.CURRENT_USER, key)
	if !ok {
		t.Fatalf("the current user's Run key must be readable")
	}

	pointScannerAt(t, []runKey{{registry.CURRENT_USER, key}}, nil, filepath.Join(t.TempDir(), "no-tasks"))

	entries, err := scanPersistence()
	if err != nil {
		t.Fatalf("scanPersistence: %v", err)
	}
	if len(entries) != len(values) {
		t.Fatalf("entries = %d, want one per Run value (%d)", len(entries), len(values))
	}

	want := make(map[string]bool, len(values))
	for _, v := range values {
		want[key+`\`+v] = true
	}
	for _, e := range entries {
		if e.kind != "runkey" {
			t.Errorf("entry %q has kind %q, want runkey", e.path, e.kind)
		}
		if !want[e.path] {
			t.Errorf("entry path %q does not name a Run key value", e.path)
		}
	}
}

// Sad path: an absent key reads as unreadable rather than as an empty key.
func TestReadRunKeyRejectsMissingKey(t *testing.T) {
	if _, ok := readRunKey(registry.CURRENT_USER, `Software\GRIMA-NoSuchKey-Sprint1`); ok {
		t.Fatal("a missing registry key must not read as readable")
	}
}

// The entries present before Start are the baseline: they are tracked and
// reported as health, but they never raise an alert.
func TestStartBaselinesExistingEntriesWithoutAlerting(t *testing.T) {
	startup := t.TempDir()
	writeFile(t, filepath.Join(startup, "already-there.lnk"))
	pointScannerAt(t, nil, []string{startup}, filepath.Join(t.TempDir(), "no-tasks"))

	src := New(config.Default(), quietLogger())
	out := make(chan event.Event, 8)

	if err := src.Start(context.Background(), out); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer src.Close()

	if tracked := src.Stats().Extra["tracked"]; tracked != 1 {
		t.Fatalf("tracked = %d, want 1 pre-existing entry", tracked)
	}

	src.poll(out)
	if len(out) != 0 {
		t.Fatalf("a pre-existing entry raised an alert: %+v", <-out)
	}
	if events := src.Stats().Events; events != 0 {
		t.Fatalf("events = %d, want 0 for pre-existing entries", events)
	}
}

// Only an entry that appears after Start is a persistence install. It is
// reported once, with the location and kind, and not again on the next scan.
func TestPollAlertsOnEntryAddedAfterStart(t *testing.T) {
	startup := t.TempDir()
	writeFile(t, filepath.Join(startup, "already-there.lnk"))
	pointScannerAt(t, nil, []string{startup}, filepath.Join(t.TempDir(), "no-tasks"))

	src := New(config.Default(), quietLogger())
	out := make(chan event.Event, 8)

	if err := src.Start(context.Background(), out); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer src.Close()

	newPath := filepath.Join(startup, "grima-sprint1-test.lnk")
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
		if ev.Persist != "startup" {
			t.Errorf("persist = %q, want startup", ev.Persist)
		}
		if ev.Source != name {
			t.Errorf("source = %q, want %q", ev.Source, name)
		}
	default:
		t.Fatal("no event for an entry created after Start")
	}

	if events := src.Stats().Events; events != 1 {
		t.Fatalf("events = %d, want 1", events)
	}

	src.poll(out)
	if len(out) != 0 {
		t.Fatalf("the same entry alerted twice: %+v", <-out)
	}
}

// Sad path at the sensor level: Start returns the scan error, so the caller
// skips the sensor instead of running one that observes nothing.
func TestStartFailsWhenNothingIsReadable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	pointScannerAt(t, nil, []string{missing}, missing)

	src := New(config.Default(), quietLogger())
	if err := src.Start(context.Background(), make(chan event.Event, 1)); err == nil {
		t.Fatal("Start must fail when no persistence location can be read")
	}
	if err := src.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
