//go:build windows

package attrib

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Access rights the audit ACE covers. Auditing writes rather than everything
// keeps a busy directory from flooding the Security log with reads.
const auditedAccess = 0x00000002 | // FILE_WRITE_DATA
	0x00000004 | // FILE_APPEND_DATA
	0x00000010 | // FILE_WRITE_EA
	0x00000100 | // FILE_WRITE_ATTRIBUTES
	0x00010000 // DELETE

const (
	aclRevision     = 2 // ACL_REVISION
	aclBufferSize   = 1024
	inheritChildren = 0x00000001 | 0x00000002 // OBJECT_INHERIT_ACE | CONTAINER_INHERIT_ACE
)

var (
	procInitializeAcl       = modadvapi32.NewProc("InitializeAcl")
	procAddAuditAccessAceEx = modadvapi32.NewProc("AddAuditAccessAceEx")
)

// setAuditACE puts a success-audit ACE for everyone on one directory, inherited
// by everything under it, so accesses to it reach the Security log.
func setAuditACE(path string) error {
	if err := enablePrivilege(seSecurityPrivilege); err != nil {
		return err
	}

	sid, err := everyoneSID()
	if err != nil {
		return err
	}

	acl, err := auditACL(sid)
	if err != nil {
		return err
	}

	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.SACL_SECURITY_INFORMATION, nil, nil, nil, acl)
}

// auditACL builds a one-entry audit ACL. The buffer backs the ACL, so it must
// outlive the call that installs it.
func auditACL(sid *windows.SID) (*windows.ACL, error) {
	buffer := make([]byte, aclBufferSize)
	acl := (*windows.ACL)(unsafe.Pointer(&buffer[0]))

	if err := initializeACL(acl, uint32(len(buffer)), aclRevision); err != nil {
		return nil, err
	}
	if err := addAuditAccessAceEx(acl, aclRevision, inheritChildren, sid, auditedAccess); err != nil {
		return nil, err
	}
	return acl, nil
}

func initializeACL(acl *windows.ACL, size, revision uint32) error {
	ok, _, callErr := procInitializeAcl.Call(
		uintptr(unsafe.Pointer(acl)), uintptr(size), uintptr(revision))
	if ok == 0 {
		return syscallError(callErr, "InitializeAcl")
	}
	return nil
}

// addAuditAccessAceEx adds one system-audit ACE. The access mask comes before
// the SID in this call, unlike the token functions next to it.
func addAuditAccessAceEx(acl *windows.ACL, revision, flags uint32, sid *windows.SID, mask uint32) error {
	ok, _, callErr := procAddAuditAccessAceEx.Call(
		uintptr(unsafe.Pointer(acl)),
		uintptr(revision),
		uintptr(flags),
		uintptr(mask),
		uintptr(unsafe.Pointer(sid)),
		1, // audit successful access
		0, // and not failed access
	)
	if ok == 0 {
		return syscallError(callErr, "AddAuditAccessAceEx")
	}
	return nil
}

// everyoneSID returns S-1-1-0, so the ACE covers every process that touches the
// directory, including one running as another user.
func everyoneSID() (*windows.SID, error) {
	var sid *windows.SID
	authority := &windows.SidIdentifierAuthority{Value: [6]byte{0, 0, 0, 0, 0, 1}}
	if err := windows.AllocateAndInitializeSid(authority, 1, 0, 0, 0, 0, 0, 0, 0, 0, &sid); err != nil {
		return nil, syscallError(err, "AllocateAndInitializeSid")
	}
	return sid, nil
}
