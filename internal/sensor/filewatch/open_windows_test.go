//go:build windows

package filewatch

import (
	"os"
	"path/filepath"
	"testing"
)

// The detector samples files to compute entropy. If that open did not share
// delete, a file being sampled could not be renamed until the handle closed —
// which breaks every editor's atomic save (write a temp file, rename it over
// the target), and made the encryption fixture non-deterministic: renames
// failed on most files while the detector was watching the tree.
//
// Reproduced in isolation before the fix: os.Open holds the file such that
// os.Rename fails with "being used by another process".
func TestOpenSharedDoesNotBlockRename(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	held, err := openShared(target)
	if err != nil {
		t.Fatalf("openShared: %v", err)
	}
	defer held.Close()

	renamed := target + ".locked"
	if err := os.Rename(target, renamed); err != nil {
		t.Fatalf("rename blocked while the file is open for sampling: %v", err)
	}

	// The handle must still be usable for the read that entropy sampling needs.
	buf := make([]byte, 7)
	if _, err := held.ReadAt(buf, 0); err != nil {
		t.Fatalf("read after rename: %v", err)
	}
	if string(buf) != "payload" {
		t.Fatalf("read %q, want payload", buf)
	}
}

// Deleting must work too: a rename over an existing target is a replace, and an
// encryptor that unlinks rather than renames must not be blocked either.
func TestOpenSharedDoesNotBlockDelete(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	held, err := openShared(target)
	if err != nil {
		t.Fatalf("openShared: %v", err)
	}
	defer held.Close()

	if err := os.Remove(target); err != nil {
		t.Fatalf("delete blocked while the file is open for sampling: %v", err)
	}
}
