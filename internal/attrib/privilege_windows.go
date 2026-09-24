//go:build windows

package attrib

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// seSecurityPrivilege is what setting an audit ACE and changing the audit policy
// require. An administrator's token holds it, disabled until asked for.
const seSecurityPrivilege = "SeSecurityPrivilege"

// isElevated reports whether the current token is elevated.
func isElevated() bool {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}

// enablePrivilege turns on one privilege held by the current token. It fails
// when the privilege is not held at all, which is what an unelevated process
// sees, so the caller can report the reason rather than a bare access denial.
func enablePrivilege(name string) error {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return fmt.Errorf("open process token: %w", err)
	}
	defer token.Close()

	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}

	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, namePtr, &luid); err != nil {
		return fmt.Errorf("look up %s: %w", name, err)
	}

	state := windows.Tokenprivileges{PrivilegeCount: 1}
	state.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}

	if err := windows.AdjustTokenPrivileges(token, false, &state, 0, nil, nil); err != nil {
		return fmt.Errorf("enable %s: %w", name, err)
	}

	// AdjustTokenPrivileges reports success even when it changed nothing, so the
	// only trustworthy answer is the token's own state afterwards.
	enabled, err := privilegeEnabled(token, luid)
	if err != nil {
		return err
	}
	if !enabled {
		return fmt.Errorf("enable %s: the process does not hold the privilege", name)
	}
	return nil
}

// privilegeEnabled reports whether the token currently holds one privilege.
func privilegeEnabled(token windows.Token, luid windows.LUID) (bool, error) {
	var size uint32
	err := windows.GetTokenInformation(token, windows.TokenPrivileges, nil, 0, &size)
	if err != nil && err != windows.ERROR_INSUFFICIENT_BUFFER {
		return false, fmt.Errorf("read token privileges: %w", err)
	}
	if size == 0 {
		return false, fmt.Errorf("read token privileges: the token reported no privileges")
	}

	buffer := make([]byte, size)
	if err := windows.GetTokenInformation(token, windows.TokenPrivileges, &buffer[0], size, &size); err != nil {
		return false, fmt.Errorf("read token privileges: %w", err)
	}

	state := (*windows.Tokenprivileges)(unsafe.Pointer(&buffer[0]))
	for _, privilege := range state.AllPrivileges() {
		if privilege.Luid == luid {
			return privilege.Attributes&windows.SE_PRIVILEGE_ENABLED != 0, nil
		}
	}
	return false, nil
}
