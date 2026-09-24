// Package event defines the frozen event contract shared by every sensor and
// every consumer above them.
package event

import (
	"strconv"
	"time"
)

// Kind identifies what happened. The vocabulary is also the alphabet for event
// n-grams, so growing it costs memory in every live process fingerprint.
type Kind uint8

const (
	KindUnknown Kind = iota
	KindProcessStart
	KindProcessExit
	KindFileCreate
	KindFileWrite
	KindFileRename
	KindFileDelete
	KindPersistenceInstall
	KindDecoyTouch
)

var kindNames = [...]string{
	"unknown",
	"process_start",
	"process_exit",
	"file_create",
	"file_write",
	"file_rename",
	"file_delete",
	"persistence_install",
	"decoy_touch",
}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "kind(" + strconv.Itoa(int(k)) + ")"
}

// IsFile reports whether the kind is a filesystem operation.
func (k Kind) IsFile() bool {
	switch k {
	case KindFileCreate, KindFileWrite, KindFileRename, KindFileDelete:
		return true
	}
	return false
}

// Event is one observable system action.
type Event struct {
	Seq    uint64 // assigned by the bus at publish
	Time   time.Time
	Kind   Kind
	Source string

	// Actor. PID is zero when the event could not be attributed.
	PID              int32
	PPID             int32
	ProcName         string
	Exe              string
	Cmdline          string
	UID              uint32
	AttribConfidence float64

	// Path. NewPath is the rename destination, empty otherwise.
	Path    string
	NewPath string

	// Entropy is zero when the sample was empty or unreadable; read zero as
	// unknown, never as low entropy.
	Bytes         int64
	Entropy       float64
	MagicMismatch bool

	DecoyID string // set when Kind == KindDecoyTouch
	Persist string // "cron" | "systemd" | "runkey" | "task" | "service"
}

// Extension returns the lowercase extension of Path, including the leading dot.
func (e Event) Extension() string {
	return Extension(e.Path)
}

// Extension returns the lowercase extension of path, including the leading dot.
// A dotfile such as ".bashrc" has no extension.
func Extension(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		switch path[i] {
		case '/', '\\':
			return ""
		case '.':
			if i == len(path)-1 {
				return ""
			}
			if i == 0 || path[i-1] == '/' || path[i-1] == '\\' {
				return ""
			}
			return lowerASCII(path[i:])
		}
	}
	return ""
}

// ParentDir returns the directory portion of path, or empty when there is none.
func ParentDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return ""
}

func lowerASCII(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if b == nil {
				b = []byte(s)
			}
			b[i] = c + ('a' - 'A')
		}
	}
	if b == nil {
		return s
	}
	return string(b)
}
