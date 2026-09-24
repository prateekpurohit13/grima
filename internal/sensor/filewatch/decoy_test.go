package filewatch

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/prateekpurohit13/grima/internal/attrib"
	"github.com/prateekpurohit13/grima/internal/decoy"
	"github.com/prateekpurohit13/grima/internal/event"
)

func TestDecoyWriteBecomesDecoyTouch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "_grima_canary.doc")
	writeTestFile(t, path, bytes.Repeat([]byte("canary"), 64))

	reg := decoy.NewRegistry()
	reg.Add(path, "canary-1")

	s := New(sensorConfig(dir), attrib.New(time.Second), reg, slog.New(slog.DiscardHandler))
	out := make(chan event.Event, 4)
	s.handle(fsnotify.Event{Name: path, Op: fsnotify.Write}, out)

	got := drain(out)
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}
	if got[0].Kind != event.KindDecoyTouch {
		t.Fatalf("kind = %s, want %s", got[0].Kind, event.KindDecoyTouch)
	}
	if got[0].DecoyID != "canary-1" {
		t.Fatalf("decoy id = %q, want %q", got[0].DecoyID, "canary-1")
	}
}

func TestWriteToANonDecoyPathStaysFileWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	writeTestFile(t, path, bytes.Repeat([]byte("hello world\n"), 64))

	reg := decoy.NewRegistry()
	reg.Add(filepath.Join(dir, "_grima_canary.doc"), "canary-1")

	s := New(sensorConfig(dir), attrib.New(time.Second), reg, slog.New(slog.DiscardHandler))
	out := make(chan event.Event, 4)
	s.handle(fsnotify.Event{Name: path, Op: fsnotify.Write}, out)

	got := drain(out)
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}
	if got[0].Kind != event.KindFileWrite {
		t.Fatalf("kind = %s, want %s", got[0].Kind, event.KindFileWrite)
	}
	if got[0].DecoyID != "" {
		t.Fatalf("decoy id = %q, want empty for a non-decoy path", got[0].DecoyID)
	}
}
