//go:build windows

package attrib

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The File System audit subcategory, and the bits of an audit policy word.
const (
	policyAuditSuccess uint32 = 0x00000001
	policyAuditFailure uint32 = 0x00000002
	policyAuditNone    uint32 = 0x00000004
)

// auditPolicyInformation mirrors AUDIT_POLICY_INFORMATION. The OS writes this
// structure directly, so its layout is asserted in the tests.
type auditPolicyInformation struct {
	SubCategoryGuid     windows.GUID
	AuditingInformation uint32
	CategoryGuid        windows.GUID
}

var (
	modadvapi32              = windows.NewLazySystemDLL("advapi32.dll")
	procAuditQueryPolicy     = modadvapi32.NewProc("AuditQuerySystemPolicy")
	procAuditSetSystemPolicy = modadvapi32.NewProc("AuditSetSystemPolicy")
	procAuditFree            = modadvapi32.NewProc("AuditFree")
)

// auditPolicyWithSuccess returns the policy word that audits successes without
// dropping what the host already audits for the subcategory.
func auditPolicyWithSuccess(current uint32) uint32 {
	if current&policyAuditNone != 0 {
		return policyAuditSuccess
	}
	return current | policyAuditSuccess
}

// fileSystemSubcategory is the GUID of the "File System" audit subcategory.
var fileSystemSubcategory = windows.GUID{
	Data1: 0x0CCE921D, Data2: 0x69AE, Data3: 0x11D9,
	Data4: [8]byte{0xBE, 0xD3, 0x50, 0x50, 0x54, 0x50, 0x30, 0x30},
}

// systemAuditPolicy returns the current policy word for the File System
// subcategory.
func systemAuditPolicy() (uint32, error) {
	if err := enablePrivilege(seSecurityPrivilege); err != nil {
		return 0, err
	}

	guid := fileSystemSubcategory
	var policy *auditPolicyInformation

	// AuditQuerySystemPolicy returns a BOOLEAN, not an error code: zero means
	// failure and non-zero means success. Reading the return value as an errno
	// makes every successful call look like error 1, "Incorrect function".
	ok, _, callErr := procAuditQueryPolicy.Call(
		uintptr(unsafe.Pointer(&guid)),
		1,
		uintptr(unsafe.Pointer(&policy)),
	)
	if ok == 0 {
		return 0, syscallError(callErr, "AuditQuerySystemPolicy")
	}
	if policy == nil {
		return 0, fmt.Errorf("AuditQuerySystemPolicy reported success but returned no policy")
	}
	// The system allocates this structure; AuditFree is its documented release.
	defer procAuditFree.Call(uintptr(unsafe.Pointer(policy)))

	return policy.AuditingInformation, nil
}

// setSystemAuditPolicy sets the policy word for the File System subcategory.
func setSystemAuditPolicy(policy uint32) error {
	if err := enablePrivilege(seSecurityPrivilege); err != nil {
		return err
	}

	info := auditPolicyInformation{
		SubCategoryGuid:     fileSystemSubcategory,
		AuditingInformation: policy,
	}

	ok, _, callErr := procAuditSetSystemPolicy.Call(uintptr(unsafe.Pointer(&info)), 1)
	if ok == 0 {
		return syscallError(callErr, "AuditSetSystemPolicy")
	}
	return nil
}
