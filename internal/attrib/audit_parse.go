package attrib

import (
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// auditEventID is the Security event that reports object access. It carries the
// process that accessed the file, which is what makes it worth parsing.
const auditEventID = 4663

// auditWriteMask is the access a 4663 event must carry to be a write. The audit
// ACE only covers these bits, and the check keeps a read that shares a handle
// with a write out of the index.
const auditWriteMask = 0x00000002 | // FILE_WRITE_DATA
	0x00000004 | // FILE_APPEND_DATA
	0x00000010 | // FILE_WRITE_EA
	0x00000100 | // FILE_WRITE_ATTRIBUTES
	0x00010000 // DELETE

// auditMaxFields bounds how many event fields are read from one record, so a
// malformed or hostile log entry cannot grow the parser's memory.
const auditMaxFields = 64

// auditEvent is the part of a Security event's XML that names the writer.
type auditEvent struct {
	System struct {
		EventID     int `xml:"EventID"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
	} `xml:"System"`
	EventData struct {
		Data []struct {
			Name  string `xml:"Name,attr"`
			Value string `xml:",chardata"`
		} `xml:"Data"`
	} `xml:"EventData"`
}

// parseAuditEvent turns one Security event into the writer it names.
//
// It reports false when the event is not a file write, which includes reads,
// non-file objects, and paths on volumes the file sensor cannot be watching.
func parseAuditEvent(xmlText string, paths *devicePaths) (Writer, bool, error) {
	var ev auditEvent
	if err := xml.Unmarshal([]byte(xmlText), &ev); err != nil {
		return Writer{}, false, fmt.Errorf("parse event xml: %w", err)
	}
	if ev.System.EventID != auditEventID {
		return Writer{}, false, nil
	}

	fields := make(map[string]string, len(ev.EventData.Data))
	for _, d := range ev.EventData.Data {
		if len(fields) >= auditMaxFields {
			break
		}
		fields[d.Name] = strings.TrimSpace(d.Value)
	}

	if kind := fields["ObjectType"]; kind != "" && !strings.EqualFold(kind, "File") {
		return Writer{}, false, nil
	}
	if text := fields["AccessMask"]; text != "" {
		mask, err := parseAuditHex(text)
		if err != nil {
			return Writer{}, false, fmt.Errorf("event %d has an unreadable access mask: %w", auditEventID, err)
		}
		if mask&auditWriteMask == 0 {
			return Writer{}, false, nil
		}
	}

	pid, err := parseAuditHex(fields["ProcessId"])
	if err != nil || pid == 0 {
		return Writer{}, false, fmt.Errorf("event %d has no usable process id", auditEventID)
	}

	path, ok := paths.toDOS(fields["ObjectName"])
	if !ok {
		return Writer{}, false, nil
	}

	at, err := time.Parse(time.RFC3339Nano, ev.System.TimeCreated.SystemTime)
	if err != nil {
		return Writer{}, false, fmt.Errorf("event %d has no usable timestamp: %w", auditEventID, err)
	}

	exe := fields["ProcessName"]
	return Writer{
		Path: path,
		PID:  int32(pid),
		Name: processName(exe),
		Exe:  exe,
		At:   at,
	}, true, nil
}

// processName reduces a process image path to the name the process sensor uses.
func processName(exe string) string {
	if exe == "" {
		return ""
	}
	return filepath.Base(exe)
}

// parseAuditHex reads the 0x-prefixed numbers the Security log writes.
func parseAuditHex(text string) (uint64, error) {
	if text == "" {
		return 0, fmt.Errorf("empty number")
	}
	return strconv.ParseUint(strings.TrimPrefix(strings.ToLower(text), "0x"), 16, 32)
}
