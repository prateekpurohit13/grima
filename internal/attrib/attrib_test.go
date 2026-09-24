package attrib

import (
	"testing"
	"time"
)

func TestSingleWriterHasFullConfidence(t *testing.T) {
	a := New(2 * time.Second)
	a.Observe(100, "crypt", 1, "/usr/bin/crypt", 4096)

	pid, name, ppid, exe, confidence := a.Suspect(time.Now(), "/tmp/watched.txt")
	if pid != 100 || name != "crypt" || ppid != 1 || exe != "/usr/bin/crypt" {
		t.Fatalf("suspect = (%d, %q, %d, %q)", pid, name, ppid, exe)
	}
	if confidence != 1 {
		t.Fatalf("confidence = %v, want 1 for a single writer", confidence)
	}
}

func TestAmbiguousWritersShareConfidence(t *testing.T) {
	a := New(2 * time.Second)
	a.Observe(1, "heavy", 0, "", 9000)
	a.Observe(2, "light", 0, "", 1000)

	pid, _, _, _, confidence := a.Suspect(time.Now(), "/tmp/watched.txt")
	if pid != 1 {
		t.Fatalf("pid = %d, want the heaviest writer 1", pid)
	}
	if confidence <= 0 || confidence >= 1 {
		t.Fatalf("confidence = %v, want strictly between 0 and 1", confidence)
	}
}

// Sad path: nothing to blame means zero confidence, not a guess.
func TestNoActivityReportsZeroConfidence(t *testing.T) {
	a := New(2 * time.Second)

	pid, _, _, _, confidence := a.Suspect(time.Now(), "/tmp/watched.txt")
	if pid != 0 || confidence != 0 {
		t.Fatalf("suspect = (%d, %v), want (0, 0)", pid, confidence)
	}
}

// Sad path: activity older than the window must not be blamed.
func TestStaleActivityIsIgnored(t *testing.T) {
	a := New(20 * time.Millisecond)
	a.Observe(5, "old", 0, "", 1000)

	time.Sleep(60 * time.Millisecond)

	pid, _, _, _, confidence := a.Suspect(time.Now(), "/tmp/watched.txt")
	if pid != 0 || confidence != 0 {
		t.Fatalf("suspect = (%d, %v), want (0, 0) after the window elapsed", pid, confidence)
	}
}

func TestZeroDeltaIsNotRecorded(t *testing.T) {
	a := New(2 * time.Second)
	a.Observe(9, "idle", 0, "", 0)

	if pid, _, _, _, _ := a.Suspect(time.Now(), "/tmp/watched.txt"); pid != 0 {
		t.Fatalf("pid = %d, want 0 when no bytes were written", pid)
	}
}

func TestForgetDropsActivity(t *testing.T) {
	a := New(2 * time.Second)
	a.Observe(42, "gone", 0, "", 5000)
	a.Forget(42)

	if pid, _, _, _, _ := a.Suspect(time.Now(), "/tmp/watched.txt"); pid != 0 {
		t.Fatalf("pid = %d, want 0 after Forget", pid)
	}
}

func TestZeroWindowFallsBackToDefault(t *testing.T) {
	a := New(0)
	if a.window <= 0 {
		t.Fatalf("window = %v, want a positive default", a.window)
	}
}
