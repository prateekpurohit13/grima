//go:build windows

package attrib

import (
	"strings"

	"golang.org/x/sys/windows"
)

// newDevicePaths asks the OS which device each drive letter stands for, so
// audited device paths can be matched against watched DOS paths.
func newDevicePaths() *devicePaths {
	paths := &devicePaths{byDevice: make(map[string]string)}

	for letter := 'A'; letter <= 'Z'; letter++ {
		drive := string(letter) + ":"
		target, err := queryDosDevice(drive)
		if err != nil || target == "" {
			continue
		}
		paths.byDevice[strings.ToLower(target)+`\`] = drive
	}
	return paths
}

func queryDosDevice(name string) (string, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}

	buf := make([]uint16, windows.MAX_PATH)
	n, err := windows.QueryDosDevice(namePtr, &buf[0], uint32(len(buf)))
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", windows.ERROR_FILE_NOT_FOUND
	}
	return windows.UTF16ToString(buf[:n]), nil
}
