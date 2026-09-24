//go:build windows

package attrib

import (
	"os"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// systemAuditAceType is SYSTEM_AUDIT_ACE_TYPE, the ACE kind the SACL needs.
const systemAuditAceType = 0x2

// aceHeader is the fixed part of any ACE, which is what GetAce hands back.
type aceHeader struct {
	AceType  byte
	AceFlags byte
	AceSize  uint16
	Mask     uint32
	SidStart uint32
}

// The policy structure is written by the OS, so its layout is a contract.
func TestAuditPolicyInformationLayout(t *testing.T) {
	var info auditPolicyInformation
	if got := unsafe.Sizeof(info); got != 36 {
		t.Fatalf("size = %d, want 36", got)
	}
	if got := unsafe.Offsetof(info.AuditingInformation); got != 16 {
		t.Fatalf("AuditingInformation offset = %d, want 16", got)
	}
	if got := unsafe.Offsetof(info.CategoryGuid); got != 20 {
		t.Fatalf("CategoryGuid offset = %d, want 20", got)
	}
}

func TestAuditSourceDescribesItsMechanism(t *testing.T) {
	src := newAuditSource(nil, false, testLogger())
	if got := src.Describe(); !strings.Contains(got, "4663") {
		t.Fatalf("Describe = %q, want it to name the event the mechanism uses", got)
	}
}

// These are winevt.h values. Treating an error notification as a delivered event
// (or the reverse) would silently discard every audited write, so they are
// pinned here rather than trusted.
func TestSubscribeActionValuesMatchTheHeader(t *testing.T) {
	if evtSubscribeActionError != 0 || evtSubscribeActionDeliver != 1 {
		t.Fatalf("actions = error %d, deliver %d; want 0 and 1",
			evtSubscribeActionError, evtSubscribeActionDeliver)
	}
	if evtSubscribeToFutureEvents != 1 || evtRenderEventXML != 1 {
		t.Fatalf("flags = future %d, xml %d; want 1 and 1",
			evtSubscribeToFutureEvents, evtRenderEventXML)
	}
}

// A delivered event is parsed; an error notification is counted, never parsed.
func TestSubscribeNotifyDispatch(t *testing.T) {
	src := newAuditSource(nil, false, testLogger())
	src.parent = testDevicePaths()
	delivered := 0
	src.deliver = func(Writer) { delivered++ }

	context := src.registerTarget()
	defer subscribeTargets.Delete(context)

	handleSubscribeNotify(evtSubscribeActionError, context, 5)
	if stats := src.Stats(); stats.Errors != 1 || stats.Records != 0 {
		t.Fatalf("stats = %+v, want the error counted and nothing delivered", stats)
	}

	// A deliver notification with no handle cannot be rendered, and must be
	// counted rather than crash the callback thread.
	handleSubscribeNotify(evtSubscribeActionDeliver, context, 0)
	handleSubscribeNotify(99, context, 0)
	if delivered != 0 {
		t.Fatalf("delivered = %d, want nothing delivered from a bad notification", delivered)
	}

	handleSubscribeNotify(evtSubscribeActionDeliver, 0, 0)
}

// The ACE is what makes the Security log report writes, so its shape matters:
// a system-audit ACE, for the write mask, inherited by children.
func TestAuditACLCarriesTheWriteAuditAce(t *testing.T) {
	sid, err := everyoneSID()
	if err != nil {
		t.Fatalf("everyone sid: %v", err)
	}
	if got := windows.GetLengthSid(sid); got != 12 {
		t.Fatalf("sid length = %d, want 12", got)
	}

	acl, err := auditACL(sid)
	if err != nil {
		t.Fatalf("build acl: %v", err)
	}
	if acl.AceCount != 1 {
		t.Fatalf("ace count = %d, want 1", acl.AceCount)
	}

	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(acl, 0, &ace); err != nil {
		t.Fatalf("GetAce: %v", err)
	}
	entry := (*aceHeader)(unsafe.Pointer(ace))
	if entry.AceType != systemAuditAceType {
		t.Fatalf("ace type = %d, want %d", entry.AceType, systemAuditAceType)
	}
	if entry.Mask != auditedAccess {
		t.Fatalf("ace mask = %#x, want %#x", entry.Mask, auditedAccess)
	}
	if entry.AceFlags&inheritChildren != inheritChildren {
		t.Fatalf("ace flags = %#x, want inheritance %#x", entry.AceFlags, inheritChildren)
	}
	if entry.AceSize < 4+4+12 {
		t.Fatalf("ace size = %d, want room for a header, a mask, and the SID", entry.AceSize)
	}
}

// The audit ACE has to reach every file under the monitored directory, so the
// audited rights must include the ones a rewrite uses.
func TestAuditedAccessCoversRewrites(t *testing.T) {
	const (
		fileWriteData       = 0x00000002
		fileAppendData      = 0x00000004
		fileWriteAttributes = 0x00000100
		remove              = 0x00010000
	)
	for _, right := range []uint32{fileWriteData, fileAppendData, fileWriteAttributes, remove} {
		if auditedAccess&right == 0 {
			t.Fatalf("audited access %#x does not cover %#x", auditedAccess, right)
		}
	}
	if auditedAccess&auditWriteMask != auditWriteMask {
		t.Fatalf("audited access %#x does not cover the write mask the parser accepts %#x", auditedAccess, auditWriteMask)
	}
}

// Sad path: unelevated, the mechanism must refuse to start with a reason an
// operator can act on, rather than starting and reporting nothing forever.
func TestAuditSourceRefusesToStartUnelevated(t *testing.T) {
	if isElevated() {
		t.Skip("elevated: starting the audit source would change this host's audit policy")
	}

	src := newAuditSource([]string{t.TempDir()}, true, testLogger())
	err := src.Start(func(Writer) {})
	if err == nil {
		t.Fatal("want an error when the process is not elevated")
	}
	if !strings.Contains(err.Error(), "elevated") {
		t.Fatalf("error = %v, want it to name elevation", err)
	}
}

// Sad path: setting the audit ACE needs a privilege the process does not hold,
// which must surface as a specific error rather than a bare failure.
func TestSetAuditACENeedsPrivilege(t *testing.T) {
	if isElevated() {
		t.Skip("elevated: setting the ACE would change this directory's SACL")
	}

	err := setAuditACE(t.TempDir())
	if err == nil {
		t.Fatal("want an error when the process cannot set an audit ACE")
	}
	if !strings.Contains(err.Error(), "SeSecurityPrivilege") {
		t.Fatalf("error = %v, want it to name the missing privilege", err)
	}
}

func TestEnableFileSystemAuditingNeedsPrivilege(t *testing.T) {
	if isElevated() {
		t.Skip("elevated: this would change the host audit policy")
	}

	err := enableFileSystemAuditing()
	if err == nil {
		t.Fatal("want an error when the process cannot change the audit policy")
	}
	if !strings.Contains(err.Error(), "SeSecurityPrivilege") {
		t.Fatalf("error = %v, want it to name the missing privilege", err)
	}
}

// The audit source reads the whole monitored tree, so a file the detector itself
// writes must not be reported as an intruder's work.
func TestAuditSourceSkipsItsOwnWrites(t *testing.T) {
	src := newAuditSource(nil, false, testLogger())
	src.parent = testDevicePaths()
	delivered := 0
	src.deliver = func(Writer) { delivered++ }

	src.handleEvent(writeEvent(map[string]string{"ProcessId": "0x1a2b"}))
	if delivered != 1 {
		t.Fatalf("delivered = %d, want the event passed on", delivered)
	}
	if stats := src.Stats(); stats.Records != 1 {
		t.Fatalf("records = %d, want 1", stats.Records)
	}

	src.handleEvent(writeEvent(map[string]string{"AccessMask": "0x1"}))
	src.handleEvent("<Event>")
	if stats := src.Stats(); stats.Skipped != 1 || stats.Errors != 1 {
		t.Fatalf("stats = %+v, want one skip and one error", stats)
	}
}

// Enabling success auditing must not throw away a host's failure auditing, and
// must not send a combination the API rejects.
func TestAuditPolicyWithSuccess(t *testing.T) {
	cases := []struct {
		name    string
		current uint32
		want    uint32
	}{
		{"nothing audited", 0, policyAuditSuccess},
		{"already on", policyAuditSuccess, policyAuditSuccess},
		{"failures kept", policyAuditFailure, policyAuditFailure | policyAuditSuccess},
		{"both kept", policyAuditFailure | policyAuditSuccess, policyAuditFailure | policyAuditSuccess},
		{"explicit none replaced", policyAuditNone, policyAuditSuccess},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := auditPolicyWithSuccess(tc.current); got != tc.want {
				t.Fatalf("policy = %#x, want %#x", got, tc.want)
			}
		})
	}
}

func TestQueryDosDeviceRejectsAnUnknownDrive(t *testing.T) {
	if _, err := queryDosDevice("QZ:"); err == nil {
		t.Skip("this host happens to have a QZ: device")
	}
	_ = os.Getenv("SystemDrive")
}
