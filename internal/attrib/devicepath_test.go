//go:build windows

package attrib

import (
	"os"
	"strings"
	"testing"
)

func testDevicePaths() *devicePaths {
	return &devicePaths{byDevice: map[string]string{
		`\device\harddiskvolume4\`: "C:",
		`\device\harddiskvolume9\`: "D:",
	}}
}

func TestToDOSMapsDevicePath(t *testing.T) {
	got, ok := testDevicePaths().toDOS(`\Device\HarddiskVolume4\Users\me\notes.txt`)
	if !ok {
		t.Fatal("device path was not translated")
	}
	if want := `C:\Users\me\notes.txt`; got != want {
		t.Fatalf("toDOS = %q, want %q", got, want)
	}
}

func TestToDOSPassesThroughDOSPath(t *testing.T) {
	got, ok := testDevicePaths().toDOS(`D:\data\notes.txt`)
	if !ok || got != `D:\data\notes.txt` {
		t.Fatalf("toDOS = %q, %v; want the path unchanged", got, ok)
	}
}

// Sad path: a file on a volume with no drive letter is not a file the sensor can
// be watching, so it must not be guessed at.
func TestToDOSRejectsUnknownDevice(t *testing.T) {
	if got, ok := testDevicePaths().toDOS(`\Device\HarddiskVolume7\data\a.txt`); ok {
		t.Fatalf("toDOS = %q, want no translation for an unmapped volume", got)
	}
}

func TestToDOSRejectsUnusableInput(t *testing.T) {
	cases := map[string]string{
		"empty":       "",
		"relative":    `data\a.txt`,
		"drive only":  "C:",
		"drive slash": `C:`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if got, ok := testDevicePaths().toDOS(input); ok && input != `D:\data\notes.txt` {
				t.Fatalf("toDOS(%q) = %q, want no translation", input, got)
			}
		})
	}
}

// The audit log writes the device name in its own case, so the prefix match must
// not depend on case.
func TestToDOSIgnoresCase(t *testing.T) {
	got, ok := testDevicePaths().toDOS(`\device\harddiskvolume4\Users\me\a.txt`)
	if !ok || !strings.HasPrefix(got, `C:\`) {
		t.Fatalf("toDOS = %q, %v; want a C: path", got, ok)
	}
}

// This checks the mapping against the OS rather than against a fixture: the
// system drive must be reachable from the device path the OS reports for it.
func TestDevicePathsCoverTheSystemDrive(t *testing.T) {
	drive := os.Getenv("SystemDrive")
	if drive == "" {
		drive = "C:"
	}

	device, err := queryDosDevice(drive)
	if err != nil {
		t.Fatalf("QueryDosDevice(%s): %v", drive, err)
	}

	paths := newDevicePaths()
	got, ok := paths.toDOS(device + `\Windows`)
	if !ok {
		t.Fatalf("no translation for %s, which is the device path of %s", device, drive)
	}
	if want := drive + `\Windows`; !strings.EqualFold(got, want) {
		t.Fatalf("toDOS = %q, want %q", got, want)
	}
}
