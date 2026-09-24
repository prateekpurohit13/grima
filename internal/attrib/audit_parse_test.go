//go:build windows

package attrib

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// auditField is one rendered <Data> element.
type auditField struct{ name, value string }

// auditXMLWith renders an event with its fields in the order given, which is the
// order the parser reads them in.
func auditXMLWith(eventID string, fields []auditField) string {
	var b strings.Builder
	b.WriteString(`<Event xmlns="http://schemas.microsoft.com/win/2004/08/events/event"><System>`)
	b.WriteString(`<Provider Name="Microsoft-Windows-Security-Auditing" Guid="{54849625-5478-4994-A5BA-3E3B0328C30D}"/>`)
	b.WriteString(`<EventID>` + eventID + `</EventID><Version>1</Version><Level>0</Level><Task>12801</Task>`)
	b.WriteString(`<TimeCreated SystemTime="2026-09-24T09:15:02.1234567Z"/>`)
	b.WriteString(`<EventRecordID>4412</EventRecordID><Channel>Security</Channel><Computer>HOST</Computer><Security/>`)
	b.WriteString(`</System><EventData>`)
	for _, field := range fields {
		b.WriteString(`<Data Name="` + field.name + `">` + field.value + `</Data>`)
	}
	b.WriteString(`</EventData></Event>`)
	return b.String()
}

// writeFields is one file write as the Security log reports it, with overrides
// applied. The field names are the ones the provider's manifest declares.
func writeFields(overrides map[string]string) []auditField {
	values := map[string]string{
		"SubjectUserSid":    "S-1-5-21-1",
		"SubjectUserName":   "Prateek",
		"SubjectDomainName": "HOST",
		"SubjectLogonId":    "0x3e7",
		"ObjectServer":      "Security",
		"ObjectType":        "File",
		"ObjectName":        `\Device\HarddiskVolume4\Users\me\notes.txt`,
		"HandleId":          "0x2c4",
		"AccessList":        "%%4417",
		"AccessMask":        "0x2",
		"ProcessId":         "0x1a2b",
		"ProcessName":       `C:\Tools\crypt.exe`,
	}
	for name, value := range overrides {
		values[name] = value
	}

	fields := make([]auditField, 0, len(values))
	for _, name := range []string{
		"SubjectUserSid", "SubjectUserName", "SubjectDomainName", "SubjectLogonId",
		"ObjectServer", "ObjectType", "ObjectName", "HandleId", "AccessList",
		"AccessMask", "ProcessId", "ProcessName",
	} {
		fields = append(fields, auditField{name, values[name]})
	}
	return fields
}

func writeEvent(overrides map[string]string) string {
	return auditXMLWith("4663", writeFields(overrides))
}

func TestParseAuditEventNamesTheWriter(t *testing.T) {
	writer, ok, err := parseAuditEvent(writeEvent(nil), testDevicePaths())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !ok {
		t.Fatal("a write to a watched volume was not recognised")
	}
	if want := `C:\Users\me\notes.txt`; writer.Path != want {
		t.Fatalf("path = %q, want %q", writer.Path, want)
	}
	if writer.PID != 0x1a2b {
		t.Fatalf("pid = %d, want %d", writer.PID, 0x1a2b)
	}
	if writer.Name != "crypt.exe" || writer.Exe != `C:\Tools\crypt.exe` {
		t.Fatalf("writer = %+v, want the process image name", writer)
	}
	if !writer.At.Equal(time.Date(2026, 9, 24, 9, 15, 2, 123456700, time.UTC)) {
		t.Fatalf("at = %s, want the event's own timestamp", writer.At)
	}
}

// Sad path: a read is not a write, and blaming the reader would be wrong.
func TestParseAuditEventSkipsReads(t *testing.T) {
	writer, ok, err := parseAuditEvent(writeEvent(map[string]string{"AccessMask": "0x1"}), testDevicePaths())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ok {
		t.Fatalf("read was treated as a write: %+v", writer)
	}
}

func TestParseAuditEventSkipsOtherEventIDs(t *testing.T) {
	_, ok, err := parseAuditEvent(auditXMLWith("4664", writeFields(nil)), testDevicePaths())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ok {
		t.Fatal("an event that is not object access was accepted")
	}
}

func TestParseAuditEventSkipsNonFileObjects(t *testing.T) {
	_, ok, err := parseAuditEvent(writeEvent(map[string]string{"ObjectType": "Key"}), testDevicePaths())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ok {
		t.Fatal("a registry key access was accepted as a file write")
	}
}

// Sad path: a path on a volume with no drive letter is not one the sensor can be
// watching, so it is skipped rather than guessed at.
func TestParseAuditEventSkipsUnmappedVolume(t *testing.T) {
	_, ok, err := parseAuditEvent(writeEvent(map[string]string{"ObjectName": `\Device\HarddiskVolume7\data\a.txt`}), testDevicePaths())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ok {
		t.Fatal("a path on an unmapped volume was accepted")
	}
}

// Sad path: an event without a process id cannot name a writer, and must be
// reported rather than silently dropped.
func TestParseAuditEventRejectsMissingProcessID(t *testing.T) {
	if _, _, err := parseAuditEvent(writeEvent(map[string]string{"ProcessId": ""}), testDevicePaths()); err == nil {
		t.Fatal("want an error for an event with no process id")
	}
}

func TestParseAuditEventRejectsUnreadableProcessID(t *testing.T) {
	if _, _, err := parseAuditEvent(writeEvent(map[string]string{"ProcessId": "not-a-number"}), testDevicePaths()); err == nil {
		t.Fatal("want an error for an unreadable process id")
	}
}

// Sad path: malformed input must return an error rather than panic, because it
// arrives on a callback thread.
func TestParseAuditEventRejectsMalformedXML(t *testing.T) {
	if _, _, err := parseAuditEvent("<Event><System>", testDevicePaths()); err == nil {
		t.Fatal("want an error for malformed XML")
	}
}

func TestParseAuditEventRejectsUnreadableTimestamp(t *testing.T) {
	broken := strings.Replace(writeEvent(nil), "2026-09-24T09:15:02.1234567Z", "not-a-time", 1)
	if _, _, err := parseAuditEvent(broken, testDevicePaths()); err == nil {
		t.Fatal("want an error for an unreadable timestamp")
	}
}

// The audit ACE only covers writes, so a missing access mask is not a reason to
// throw the event away.
func TestParseAuditEventAcceptsMissingAccessMask(t *testing.T) {
	_, ok, err := parseAuditEvent(writeEvent(map[string]string{"AccessMask": ""}), testDevicePaths())
	if err != nil || !ok {
		t.Fatalf("ok = %v, err = %v; want the event accepted", ok, err)
	}
}

// Some access events carry no image name; the pid is still worth keeping.
func TestParseAuditEventWithoutProcessName(t *testing.T) {
	writer, ok, err := parseAuditEvent(writeEvent(map[string]string{"ProcessName": ""}), testDevicePaths())
	if err != nil || !ok {
		t.Fatalf("ok = %v, err = %v; want the event accepted", ok, err)
	}
	if writer.PID != 0x1a2b || writer.Name != "" {
		t.Fatalf("writer = %+v, want a pid and no name", writer)
	}
}

// A record with far more fields than a real one must be read in bounded work,
// and the fields that matter must still be found when they come first.
func TestParseAuditEventReadsPastFiller(t *testing.T) {
	fields := writeFields(nil)
	for i := 0; i < auditMaxFields*4; i++ {
		fields = append(fields, auditField{fmt.Sprintf("Filler%03d", i), "x"})
	}

	writer, ok, err := parseAuditEvent(auditXMLWith("4663", fields), testDevicePaths())
	if err != nil || !ok {
		t.Fatalf("ok = %v, err = %v; want the event accepted", ok, err)
	}
	if writer.PID != 0x1a2b {
		t.Fatalf("pid = %d, want the writer named before the filler", writer.PID)
	}
}

// Sad path: the parser stops at its field bound, so a hostile record cannot make
// it read without limit. Fields past the bound are ignored, which is why the
// bound is generous compared with a real event.
func TestParseAuditEventStopsAtFieldBound(t *testing.T) {
	var fields []auditField
	for i := 0; i < auditMaxFields*4; i++ {
		fields = append(fields, auditField{fmt.Sprintf("Filler%03d", i), "x"})
	}
	fields = append(fields, writeFields(nil)...)

	if _, ok, err := parseAuditEvent(auditXMLWith("4663", fields), testDevicePaths()); ok && err == nil {
		t.Fatal("want the event past the field bound to be refused")
	}
}

func TestParseAuditHex(t *testing.T) {
	cases := map[string]uint64{
		"0x1a2b": 6699,
		"0x2":    2,
		"0":      0,
		"0xFF":   255,
	}
	for input, want := range cases {
		got, err := parseAuditHex(input)
		if err != nil || got != want {
			t.Fatalf("parseAuditHex(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
	if _, err := parseAuditHex(""); err == nil {
		t.Fatal("want an error for an empty number")
	}
}
