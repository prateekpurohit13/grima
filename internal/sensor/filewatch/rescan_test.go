package filewatch

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/prateekpurohit13/grima/internal/attrib"
	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/decoy"
	"github.com/prateekpurohit13/grima/internal/event"
)

// sensorConfig returns a configuration that watches dir with a small entropy
// sample and overflow rescan on.
func sensorConfig(dir string) config.Config {
	cfg := config.Default()
	cfg.General.MonitorPaths = []string{dir}
	cfg.FileWatch.EntropySampleBytes = 4096
	cfg.FileWatch.RescanOnOverflow = true
	return cfg
}

// testSource returns a source that watches dir, with a logger that keeps test
// output clean.
func testSource(t *testing.T, dir string) *Source {
	t.Helper()
	return New(sensorConfig(dir), attrib.New(time.Second), decoy.NewRegistry(), slog.New(slog.DiscardHandler))
}

func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// drain returns the events emitted so far without blocking.
func drain(out chan event.Event) []event.Event {
	var got []event.Event
	for {
		select {
		case ev := <-out:
			got = append(got, ev)
		default:
			return got
		}
	}
}

func TestRescanEmitsOneEventPerRecentFile(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	content := bytes.Repeat([]byte("abcdefgh"), 512)
	writeTestFile(t, filepath.Join(dir, "one.txt"), content)
	writeTestFile(t, filepath.Join(dir, "two.txt"), content)
	writeTestFile(t, filepath.Join(sub, "three.txt"), content)

	s := testSource(t, dir)
	out := make(chan event.Event, 8)
	s.rescan(out)

	got := drain(out)
	if len(got) != 3 {
		t.Fatalf("events = %d, want 3", len(got))
	}

	seen := make(map[string]bool, len(got))
	for _, ev := range got {
		if ev.Kind != event.KindFileWrite {
			t.Fatalf("kind for %s = %s, want %s", ev.Path, ev.Kind, event.KindFileWrite)
		}
		if ev.Source != name {
			t.Fatalf("source = %q, want %q", ev.Source, name)
		}
		if ev.Entropy == 0 {
			t.Fatalf("entropy for %s = 0, want a sampled value", ev.Path)
		}
		if ev.Bytes != int64(len(content)) {
			t.Fatalf("bytes for %s = %d, want %d", ev.Path, ev.Bytes, len(content))
		}
		seen[ev.Path] = true
	}

	for _, want := range []string{
		filepath.Join(dir, "one.txt"),
		filepath.Join(dir, "two.txt"),
		filepath.Join(sub, "three.txt"),
	} {
		if !seen[want] {
			t.Fatalf("no event for %s; got %v", want, seen)
		}
	}

	if n := s.Stats().Extra["rescans"]; n != 1 {
		t.Fatalf("rescans = %d, want 1", n)
	}
}

func TestRescanIgnoresFilesOlderThanWindow(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	writeTestFile(t, old, bytes.Repeat([]byte("abcdefgh"), 512))

	stale := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, stale, stale); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	s := testSource(t, dir)
	out := make(chan event.Event, 8)
	s.rescan(out)

	if got := drain(out); len(got) != 0 {
		t.Fatalf("events = %d, want 0 for a file outside the rescan window", len(got))
	}
	if n := s.Stats().Extra["rescans"]; n != 1 {
		t.Fatalf("rescans = %d, want 1", n)
	}
}

func TestRescanContinuesPastAnUnreadableDirectory(t *testing.T) {
	dir := t.TempDir()
	visible := filepath.Join(dir, "visible.txt")
	writeTestFile(t, visible, bytes.Repeat([]byte("abcdefgh"), 512))

	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeTestFile(t, filepath.Join(locked, "hidden.txt"), bytes.Repeat([]byte("abcdefgh"), 512))
	lockDirectory(t, locked)

	s := testSource(t, dir)
	out := make(chan event.Event, 8)
	s.rescan(out)

	got := drain(out)
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1 from the readable directory", len(got))
	}
	if got[0].Path != visible {
		t.Fatalf("path = %s, want %s", got[0].Path, visible)
	}
}

func TestOverflowErrorTriggersRescan(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "one.txt"), bytes.Repeat([]byte("abcdefgh"), 512))

	s := testSource(t, dir)
	out := make(chan event.Event, 8)
	s.handleWatchError(fsnotify.ErrEventOverflow, out)

	got := drain(out)
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1 synthetic write", len(got))
	}
	if got[0].Kind != event.KindFileWrite {
		t.Fatalf("kind = %s, want %s", got[0].Kind, event.KindFileWrite)
	}

	stats := s.Stats()
	if stats.Errors != 1 {
		t.Fatalf("errors = %d, want 1", stats.Errors)
	}
	if n := stats.Extra["overflow"]; n != 1 {
		t.Fatalf("overflow = %d, want 1", n)
	}
	if n := stats.Extra["rescans"]; n != 1 {
		t.Fatalf("rescans = %d, want 1", n)
	}
}

func TestOverflowWithoutRescanSettingEmitsNothing(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "one.txt"), bytes.Repeat([]byte("abcdefgh"), 512))

	s := testSource(t, dir)
	s.cfg.FileWatch.RescanOnOverflow = false

	out := make(chan event.Event, 8)
	s.handleWatchError(fsnotify.ErrEventOverflow, out)

	if got := drain(out); len(got) != 0 {
		t.Fatalf("events = %d, want 0 with rescan_on_overflow off", len(got))
	}

	stats := s.Stats()
	if stats.Errors != 1 {
		t.Fatalf("errors = %d, want 1", stats.Errors)
	}
	if n := stats.Extra["rescans"]; n != 0 {
		t.Fatalf("rescans = %d, want 0", n)
	}
}

func TestWatchErrorOtherThanOverflowDoesNotRescan(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "one.txt"), bytes.Repeat([]byte("abcdefgh"), 512))

	s := testSource(t, dir)
	out := make(chan event.Event, 8)
	s.handleWatchError(errors.New("watch descriptor is invalid"), out)

	if got := drain(out); len(got) != 0 {
		t.Fatalf("events = %d, want 0 for a non-overflow error", len(got))
	}

	stats := s.Stats()
	if stats.Errors != 1 {
		t.Fatalf("errors = %d, want 1", stats.Errors)
	}
	if n := stats.Extra["overflow"]; n != 0 {
		t.Fatalf("overflow = %d, want 0", n)
	}
	if n := stats.Extra["rescans"]; n != 0 {
		t.Fatalf("rescans = %d, want 0", n)
	}
}
