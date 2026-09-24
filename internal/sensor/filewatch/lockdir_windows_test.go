//go:build windows

package filewatch

import (
	"os"
	"syscall"
	"testing"
)

// lockDirectory holds dir open without sharing, so listing its contents fails
// the way an unreadable directory does. The test is skipped on a host where
// that has no effect.
func lockDirectory(t *testing.T, dir string) {
	t.Helper()

	path, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatalf("encode path %s: %v", dir, err)
	}

	h, err := syscall.CreateFile(path, syscall.GENERIC_READ, 0, nil,
		syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatalf("lock %s: %v", dir, err)
	}
	t.Cleanup(func() { syscall.CloseHandle(h) })

	if _, err := os.ReadDir(dir); err == nil {
		t.Skipf("%s is still readable, cannot test an unreadable directory", dir)
	}
}
