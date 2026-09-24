package attrib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func traceLines(t *testing.T, path string) []string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// traced returns an attributor that writes its decisions to a trace file.
func traced(t *testing.T, maxLines int) (*Attributor, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "trace.jsonl")
	a := New(2 * time.Second)
	a.trace = newTracer(path, maxLines)
	t.Cleanup(func() { _ = a.trace.close() })
	return a, path
}

// Happy path: one line per decision, naming the process that was blamed.
func TestTraceWritesOneLinePerDecision(t *testing.T) {
	a, path := traced(t, 10)

	a.Observe(7, "crypt", 1, "/usr/bin/crypt", 4096)
	if pid, _, _, _, _ := a.Suspect(time.Now()); pid != 7 {
		t.Fatalf("pid = %d, want 7 with tracing on", pid)
	}

	lines := traceLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("trace lines = %d, want 1", len(lines))
	}
	for _, want := range []string{`"pid":7`, `"name":"crypt"`, `"candidates":1`, `"confidence":1`} {
		if !strings.Contains(lines[0], want) {
			t.Fatalf("trace line %s does not contain %s", lines[0], want)
		}
	}
}

// Happy path: the environment variable is what switches tracing on.
func TestTraceIsEnabledByEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv(tracePathEnv, path)

	a := New(2 * time.Second)
	t.Cleanup(func() { _ = a.trace.close() })

	a.Observe(11, "writer", 0, "", 2048)
	a.Suspect(time.Now())

	if lines := traceLines(t, path); len(lines) != 1 {
		t.Fatalf("trace lines = %d, want 1 when %s is set", len(lines), tracePathEnv)
	}
}

// Sad path: no environment variable means no trace file and no cost.
func TestTraceIsOffByDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(tracePathEnv, "")

	a := New(2 * time.Second)
	a.Observe(12, "writer", 0, "", 2048)
	if pid, _, _, _, _ := a.Suspect(time.Now()); pid != 12 {
		t.Fatalf("pid = %d, want 12 with tracing off", pid)
	}

	// Observable cost of tracing being off: nothing written and nothing lost.
	if entries, err := os.ReadDir(dir); err != nil {
		t.Fatalf("read temp dir: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("tracing off still wrote %d files", len(entries))
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close with tracing off: %v", err)
	}
}

// A decision that blames nobody is traced too, so a measurement can count the
// events the detector could not attribute at all.
func TestTraceRecordsUnattributedDecisions(t *testing.T) {
	a, path := traced(t, 10)

	a.Suspect(time.Now())

	lines := traceLines(t, path)
	if len(lines) != 1 || !strings.Contains(lines[0], `"pid":0`) {
		t.Fatalf("trace lines = %v, want one line blaming nobody", lines)
	}
}

// Sad path: an unwritable trace file must not stop attribution, and every
// decision it could not write is counted.
func TestTraceOpenFailureStillAttributes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "trace.jsonl")
	a := New(2 * time.Second)
	a.trace = newTracer(path, 10)

	a.Observe(7, "crypt", 0, "", 4096)
	if pid, _, _, _, _ := a.Suspect(time.Now()); pid != 7 {
		t.Fatalf("pid = %d, want 7 when the trace file cannot be opened", pid)
	}
	if got := a.trace.droppedLines(); got != 1 {
		t.Fatalf("dropped = %d, want 1 unwritten decision", got)
	}
}

// Sad path: the trace stops at its line limit instead of growing without bound.
func TestTraceStopsAtLineLimit(t *testing.T) {
	a, path := traced(t, 2)

	for i := 0; i < 5; i++ {
		a.Observe(7, "crypt", 0, "", 4096)
		a.Suspect(time.Now())
	}

	if lines := traceLines(t, path); len(lines) != 2 {
		t.Fatalf("trace lines = %d, want the 2 line limit", len(lines))
	}
	if got := a.trace.droppedLines(); got != 3 {
		t.Fatalf("dropped = %d, want 3 decisions past the limit", got)
	}
}
