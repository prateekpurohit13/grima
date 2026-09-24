package attrib

import (
	"fmt"
	"testing"
	"time"
)

func TestCausalRecordAnswersForTheWriteItExplains(t *testing.T) {
	index := newCausalIndex(2*time.Second, 999)
	at := time.Now()
	index.record(Writer{Path: `C:\data\notes.txt`, PID: 4242, Name: "crypt.exe", At: at})

	got, ok := index.match(`C:\data\notes.txt`, at)
	if !ok {
		t.Fatal("no match for a recorded write")
	}
	if got.PID != 4242 || got.Name != "crypt.exe" {
		t.Fatalf("match = %+v, want the recorded writer", got)
	}
}

// Windows paths are case-insensitive, and the audited path and the watched path
// do not have to agree on case.
func TestCausalMatchIgnoresCase(t *testing.T) {
	index := newCausalIndex(2*time.Second, 999)
	at := time.Now()
	index.record(Writer{Path: `C:\Users\Me\Notes.TXT`, PID: 7, At: at})

	if _, ok := index.match(`c:\users\me\notes.txt`, at); !ok {
		t.Fatal("no match for the same path in different case")
	}
}

func TestCausalMatchPrefersTheNewestRecord(t *testing.T) {
	index := newCausalIndex(2*time.Second, 999)
	at := time.Now()
	index.record(Writer{Path: `C:\a.txt`, PID: 1, At: at.Add(-200 * time.Millisecond)})
	index.record(Writer{Path: `C:\a.txt`, PID: 2, At: at.Add(-10 * time.Millisecond)})

	got, ok := index.match(`C:\a.txt`, at)
	if !ok || got.PID != 2 {
		t.Fatalf("match = %+v, %v; want the newest writer", got, ok)
	}
}

// Sad path: a record from long before the file event must not be blamed for it.
func TestCausalMatchIgnoresStaleRecords(t *testing.T) {
	index := newCausalIndex(time.Second, 999)
	index.record(Writer{Path: `C:\a.txt`, PID: 1, At: time.Now().Add(-time.Minute)})

	if got, ok := index.match(`C:\a.txt`, time.Now()); ok {
		t.Fatalf("match = %+v, want no match for a record outside the window", got)
	}
}

// Sad path: a record from far in the future cannot explain this change either.
func TestCausalMatchIgnoresRecordsFromTheFuture(t *testing.T) {
	index := newCausalIndex(time.Second, 999)
	at := time.Now()
	index.record(Writer{Path: `C:\a.txt`, PID: 1, At: at.Add(time.Minute)})

	if _, ok := index.match(`C:\a.txt`, at); ok {
		t.Fatal("want no match for a record beyond the clock skew")
	}
}

// The detector reads the files it is watching, so its own writes and reads must
// never be mistaken for the writer.
func TestCausalIndexIgnoresOwnWrites(t *testing.T) {
	index := newCausalIndex(time.Second, 4242)
	at := time.Now()
	index.record(Writer{Path: `C:\a.txt`, PID: 4242, At: at})

	if _, ok := index.match(`C:\a.txt`, at); ok {
		t.Fatal("the detector blamed itself")
	}
	if _, _, ignored := index.counters(); ignored != 1 {
		t.Fatalf("ignored = %d, want 1", ignored)
	}
}

// Sad path: a record without a path or a process cannot be used, and must not
// create an entry that can never be matched.
func TestCausalIndexIgnoresUnusableRecords(t *testing.T) {
	index := newCausalIndex(time.Second, 999)
	index.record(Writer{PID: 5, At: time.Now()})
	index.record(Writer{Path: `C:\a.txt`, At: time.Now()})

	records, _, ignored := index.counters()
	if records != 0 {
		t.Fatalf("records = %d, want 0", records)
	}
	if ignored != 2 {
		t.Fatalf("ignored = %d, want 2", ignored)
	}
}

func TestCausalAwaitReturnsWhenTheRecordArrives(t *testing.T) {
	index := newCausalIndex(2*time.Second, 999)
	at := time.Now()

	go func() {
		time.Sleep(20 * time.Millisecond)
		index.record(Writer{Path: `C:\a.txt`, PID: 11, At: at})
	}()

	start := time.Now()
	if !index.await(`C:\a.txt`, at, 5*time.Second) {
		t.Fatal("await gave up before the record arrived")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("await took %s, want it to return as soon as the record landed", elapsed)
	}
}

// Sad path: a record that never arrives must not hold the event forever.
func TestCausalAwaitGivesUpAtTheDeadline(t *testing.T) {
	index := newCausalIndex(2*time.Second, 999)

	start := time.Now()
	if index.await(`C:\a.txt`, time.Now(), 50*time.Millisecond) {
		t.Fatal("await reported a record that was never recorded")
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("await returned after %s, want it to wait for the full delay", elapsed)
	}
}

func TestCausalAwaitFindsARecordAlreadyRecorded(t *testing.T) {
	index := newCausalIndex(2*time.Second, 999)
	at := time.Now()
	index.record(Writer{Path: `C:\a.txt`, PID: 3, At: at})

	start := time.Now()
	if !index.await(`C:\a.txt`, at, time.Second) {
		t.Fatal("await missed a record that was already there")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("await waited %s for a record it already had", elapsed)
	}
}

// Bounded state: a storm over a large tree must not grow the index without end.
func TestCausalIndexStopsGrowing(t *testing.T) {
	index := newCausalIndex(2*time.Second, 999)
	at := time.Now()

	for i := 0; i < causalMaxPaths+causalPruneEvery; i++ {
		index.record(Writer{Path: fmt.Sprintf(`C:\data\file-%d.txt`, i), PID: 5, At: at})
	}

	_, dropped, _ := index.counters()
	if dropped == 0 {
		t.Fatal("want records dropped once the index was full")
	}
	index.mu.Lock()
	tracked := len(index.byPath)
	index.mu.Unlock()
	if tracked > causalMaxPaths {
		t.Fatalf("index holds %d paths, want at most %d", tracked, causalMaxPaths)
	}
}

func TestCausalIndexPrunesExpiredPaths(t *testing.T) {
	index := newCausalIndex(time.Second, 999)
	old := time.Now().Add(-time.Hour)

	for i := 0; i < causalPruneEvery+1; i++ {
		index.record(Writer{Path: fmt.Sprintf(`C:\data\file-%d.txt`, i), PID: 5, At: old})
	}

	index.mu.Lock()
	tracked := len(index.byPath)
	index.mu.Unlock()
	if tracked > causalPerPath {
		t.Fatalf("index holds %d expired paths, want the sweep to have dropped them", tracked)
	}
}

func TestCausalPerPathHistoryIsBounded(t *testing.T) {
	index := newCausalIndex(2*time.Second, 999)
	at := time.Now()

	for i := 0; i < causalPerPath*4; i++ {
		index.record(Writer{Path: `C:\a.txt`, PID: int32(i + 1), At: at})
	}

	index.mu.Lock()
	history := len(index.byPath[causalKey(`C:\a.txt`)])
	index.mu.Unlock()
	if history != causalPerPath {
		t.Fatalf("history = %d, want %d", history, causalPerPath)
	}
}

func TestCausalKeyTrimsAndLowercases(t *testing.T) {
	if got := causalKey(`  C:\Data\A.TXT  `); got != `c:\data\a.txt` {
		t.Fatalf("causalKey = %q", got)
	}
}
