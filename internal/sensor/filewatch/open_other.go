//go:build !windows

package filewatch

import "os"

// openShared opens a file for reading.
//
// POSIX has no share modes: an open handle never blocks a rename or delete, so
// the Windows problem this exists to solve does not arise here.
func openShared(path string) (*os.File, error) {
	return os.Open(path)
}
