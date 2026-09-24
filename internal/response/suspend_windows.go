//go:build windows

package response

import (
	"fmt"

	"golang.org/x/sys/windows"
)

var ntSuspendProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtSuspendProcess")

// suspendProcess stops the target process from being scheduled.
func suspendProcess(pid int32) error {
	const processSuspendResume = 0x0800

	handle, err := windows.OpenProcess(processSuspendResume, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open process: %w", err)
	}
	defer windows.CloseHandle(handle)

	if status, _, callErr := ntSuspendProcess.Call(uintptr(handle)); status != 0 {
		return fmt.Errorf("NtSuspendProcess returned %#x: %w", status, callErr)
	}
	return nil
}
