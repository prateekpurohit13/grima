//go:build windows

package filewatch

import (
	"os"

	"golang.org/x/sys/windows"
)

// openShared opens a file for reading without blocking a concurrent rename or
// delete.
//
// os.Open asks for FILE_SHARE_READ|FILE_SHARE_WRITE only, so a file the detector
// is sampling cannot be renamed until the handle closes. That breaks every
// editor's atomic save — write a temp file, rename it over the target — and it
// made the encryption fixture non-deterministic, with renames failing on most
// files while the detector was watching. A detector that stops applications
// from renaming files is a worse problem than the one it watches for.
func openShared(path string) (*os.File, error) {
	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}

	handle, err := windows.CreateFile(
		pathp,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
