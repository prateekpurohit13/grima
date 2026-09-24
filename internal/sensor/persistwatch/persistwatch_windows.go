//go:build windows

package persistwatch

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// runKey is one registry location that starts a program at logon.
type runKey struct {
	root registry.Key
	path string
}

// runKeys are the four Run/RunOnce keys. They are variables so a test can point
// the scanner at a location it controls.
var runKeys = []runKey{
	{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`},
	{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\RunOnce`},
	{registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Run`},
	{registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\RunOnce`},
}

// startupDirs are the per-user and all-users Startup folders.
var startupDirs = func() []string {
	var dirs []string
	if appData := os.Getenv("APPDATA"); appData != "" {
		dirs = append(dirs, filepath.Join(appData,
			`Microsoft\Windows\Start Menu\Programs\Startup`))
	}
	if programData := os.Getenv("ProgramData"); programData != "" {
		dirs = append(dirs, filepath.Join(programData,
			`Microsoft\Windows\Start Menu\Programs\StartUp`))
	}
	return dirs
}()

// tasksDir is the scheduled-task store.
var tasksDir = `C:\Windows\System32\Tasks`

// scanPersistence lists registry run keys, the Startup folders, and the
// scheduled task store. It reports an error only when nothing at all could be
// read, so one denied location degrades coverage instead of stopping the sensor.
func scanPersistence() ([]entry, error) {
	var (
		out      []entry
		readable int
	)

	for _, rk := range runKeys {
		if values, ok := readRunKey(rk.root, rk.path); ok {
			readable++
			for _, v := range values {
				out = append(out, entry{
					path: rk.path + `\` + v,
					kind: "runkey",
				})
			}
		}
	}

	for _, dir := range startupDirs {
		if items, err := os.ReadDir(dir); err == nil {
			readable++
			for _, it := range items {
				if it.IsDir() {
					continue
				}
				out = append(out, entry{path: filepath.Join(dir, it.Name()), kind: "startup"})
			}
		}
	}

	if items, err := os.ReadDir(tasksDir); err == nil {
		readable++
		for _, it := range items {
			if it.IsDir() {
				continue
			}
			out = append(out, entry{path: filepath.Join(tasksDir, it.Name()), kind: "task"})
		}
	}

	if readable == 0 {
		return nil, fmt.Errorf("no persistence location could be read")
	}
	return out, nil
}

func readRunKey(root registry.Key, path string) ([]string, bool) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return nil, false
	}
	defer key.Close()

	names, err := key.ReadValueNames(0)
	if err != nil {
		return nil, false
	}
	return names, true
}
