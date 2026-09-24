package attrib

import "strings"

// devicePaths maps an NT device path such as
// \Device\HarddiskVolume4\Users\me\notes.txt to the DOS path the file sensor
// reports, such as C:\Users\me\notes.txt. Auditing and ETW both name files by
// device path, and the sensor names them by drive letter.
type devicePaths struct {
	// byDevice holds lowercased device prefixes, each with its trailing
	// separator, mapped to the drive they belong to: "\device\harddiskvolume4\" -> "C:".
	byDevice map[string]string
}

// toDOS translates one NT device path. It reports false for a path on a volume
// that has no drive letter, which is a file the sensor cannot be watching.
func (p *devicePaths) toDOS(ntPath string) (string, bool) {
	if p == nil || ntPath == "" {
		return "", false
	}
	if isDOSPath(ntPath) {
		return ntPath, true
	}

	lower := strings.ToLower(ntPath)
	for device, drive := range p.byDevice {
		if !strings.HasPrefix(lower, device) {
			continue
		}
		return drive + ntPath[len(device)-1:], true
	}
	return "", false
}

// isDOSPath reports whether path already starts with a drive letter.
func isDOSPath(path string) bool {
	if len(path) < 3 || path[1] != ':' || (path[2] != '\\' && path[2] != '/') {
		return false
	}
	c := path[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
