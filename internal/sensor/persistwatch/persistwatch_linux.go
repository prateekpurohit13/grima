//go:build linux

package persistwatch

import (
	"fmt"
	"os"
	"path/filepath"
)

// cronDirs and unitDirs are the persistence locations GRIMA watches on Linux.
var cronDirs = []string{
	"/etc/cron.d",
	"/etc/cron.hourly",
	"/etc/cron.daily",
	"/var/spool/cron",
	"/var/spool/cron/crontabs",
}

var unitDirs = []string{
	"/etc/systemd/system",
	"/etc/systemd/user",
	"/lib/systemd/system",
	"/usr/lib/systemd/system",
}

var cronFiles = []string{
	"/etc/crontab",
	"/etc/rc.local",
	"/etc/anacrontab",
}

// scanPersistence lists cron entries and systemd units. It reports an error only
// when nothing at all could be read, so a single unreadable directory does not
// take the sensor down.
func scanPersistence() ([]entry, error) {
	var (
		out      []entry
		readable int
	)

	for _, dir := range cronDirs {
		if n, ok := scanDir(dir, "cron", &out); ok {
			readable += n
		}
	}
	for _, dir := range unitDirs {
		if n, ok := scanDir(dir, "systemd", &out); ok {
			readable += n
		}
	}
	for _, file := range cronFiles {
		if _, err := os.Stat(file); err == nil {
			out = append(out, entry{path: file, kind: "cron"})
			readable++
		}
	}

	if readable == 0 {
		return nil, fmt.Errorf("no persistence location could be read")
	}
	return out, nil
}

func scanDir(dir, kind string, out *[]entry) (int, bool) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return 0, false
	}
	for _, it := range items {
		if it.IsDir() {
			continue
		}
		*out = append(*out, entry{path: filepath.Join(dir, it.Name()), kind: kind})
	}
	return len(items), true
}
