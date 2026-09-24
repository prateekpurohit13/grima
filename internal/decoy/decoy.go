// Package decoy plants canary files and records which paths are canaries.
//
// A decoy is the highest-precision signal in the system: no legitimate process
// has business touching a file the user never created. Decoys are planted by
// this package and detected by the file sensor, which consults the registry.
package decoy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/prateekpurohit13/grima/internal/config"
)

const decoyBody = "Confidential - internal reference copy. Do not modify.\n"

// Registry records which paths are decoy files.
type Registry struct {
	mu     sync.RWMutex
	byPath map[string]string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byPath: make(map[string]string)}
}

// Add registers a decoy path with an identifier used for post-incident
// attribution.
func (r *Registry) Add(path, id string) {
	r.mu.Lock()
	r.byPath[filepath.Clean(path)] = id
	r.mu.Unlock()
}

// Lookup reports whether a path is a decoy and returns its identifier.
func (r *Registry) Lookup(path string) (string, bool) {
	r.mu.RLock()
	id, ok := r.byPath[filepath.Clean(path)]
	r.mu.RUnlock()
	return id, ok
}

// Count returns how many decoys are registered.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byPath)
}

// Plant writes canary files into the monitored directories and registers them.
//
// Directories that cannot be written are skipped rather than failing the run:
// planting decoys in some directories is still useful, and an unreadable system
// directory must not stop the detector from starting.
func Plant(cfg config.Config, reg *Registry) (int, error) {
	if !cfg.Decoy.Enabled || len(cfg.Decoy.Names) == 0 {
		return 0, nil
	}

	const maxDepth = 3

	planted := 0
	var firstErr error

	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxDepth {
			return
		}
		items, err := os.ReadDir(dir)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("read %s: %w", dir, err)
			}
			return
		}

		planted += plantIn(dir, cfg.Decoy, reg)

		for _, it := range items {
			if !it.IsDir() {
				continue
			}
			name := it.Name()
			if name[0] == '.' || name == "node_modules" || name == "AppData" {
				continue
			}
			walk(filepath.Join(dir, name), depth+1)
		}
	}

	for _, root := range cfg.General.MonitorPaths {
		walk(root, 0)
	}

	return planted, firstErr
}

func plantIn(dir string, dc config.DecoyConfig, reg *Registry) int {
	planted := 0
	for i, name := range dc.Names {
		if i >= dc.CountPerDir {
			break
		}
		path := filepath.Join(dir, name)

		if _, err := os.Stat(path); err == nil {
			reg.Add(path, decoyID(path)) // already there from a previous run
			planted++
			continue
		}
		if err := os.WriteFile(path, []byte(decoyBody), 0o644); err != nil {
			continue
		}
		reg.Add(path, decoyID(path))
		planted++
	}
	return planted
}

// decoyID is a short stable hash of the path, so a touched decoy can be named
// without exposing the full path in alerts.
func decoyID(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:4])
}
