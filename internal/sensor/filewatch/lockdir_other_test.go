//go:build !windows

package filewatch

import (
	"os"
	"testing"
)

// lockDirectory removes read permission from dir, so listing its contents fails
// the way an unreadable directory does. A process that can read any directory
// (root) skips the test instead.
func lockDirectory(t *testing.T, dir string) {
	t.Helper()

	if err := os.Chmod(dir, 0); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	if _, err := os.ReadDir(dir); err == nil {
		t.Skipf("running with permission to read %s, cannot test an unreadable directory", dir)
	}
}
