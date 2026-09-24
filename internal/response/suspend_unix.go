//go:build !windows

package response

import (
	"fmt"
	"syscall"
)

// suspendProcess stops the target process from being scheduled.
func suspendProcess(pid int32) error {
	if err := syscall.Kill(int(pid), syscall.SIGSTOP); err != nil {
		return fmt.Errorf("SIGSTOP: %w", err)
	}
	return nil
}
