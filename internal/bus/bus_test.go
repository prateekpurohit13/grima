package bus

import (
	"testing"
	"time"

	"github.com/prateekpurohit13/grima/internal/event"
)

func waitFor(t *testing.T, ok func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPublishReachesEverySubscriber(t *testing.T) {
	b := New(16, DropOldest)
	defer b.Close()

	first := b.Subscribe("first", 16)
	second := b.Subscribe("second", 16)

	b.Publish(event.Event{Kind: event.KindFileWrite, Path: "/tmp/a"})

	for name, ch := range map[string]<-chan event.Event{"first": first, "second": second} {
		select {
		case ev := <-ch:
			if ev.Kind != event.KindFileWrite {
				t.Fatalf("%s: kind = %v, want file_write", name, ev.Kind)
			}
			if ev.Seq == 0 {
				t.Fatalf("%s: sequence number was not assigned", name)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s: no event delivered", name)
		}
	}
}

// Sad path: a subscriber that never drains must lose events rather than block
// the publisher, and the loss must be counted.
func TestSlowSubscriberDropsAreCounted(t *testing.T) {
	b := New(64, DropNewest)
	defer b.Close()

	b.Subscribe("slow", 1) // never read from

	for range 50 {
		b.Publish(event.Event{Kind: event.KindFileWrite})
	}

	waitFor(t, func() bool { return b.Stats().SubDropped["slow"] > 0 }, "subscriber drops")
}

// Sad path: publishing after Close must be counted, not panicked on.
func TestPublishAfterCloseIsCounted(t *testing.T) {
	b := New(8, DropOldest)
	b.Close()

	before := b.Stats().Dropped
	b.Publish(event.Event{Kind: event.KindFileWrite})

	if got := b.Stats().Dropped; got != before+1 {
		t.Fatalf("dropped = %d, want %d", got, before+1)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	b := New(8, DropOldest)
	b.Close()
	b.Close()
}

func TestParseDropPolicy(t *testing.T) {
	cases := map[string]struct {
		want DropPolicy
		ok   bool
	}{
		"drop_oldest": {DropOldest, true},
		"drop_newest": {DropNewest, true},
		"block":       {Block, true},
		"nonsense":    {DropOldest, false},
	}
	for in, want := range cases {
		got, ok := ParseDropPolicy(in)
		if ok != want.ok || got != want.want {
			t.Fatalf("ParseDropPolicy(%q) = (%v, %v), want (%v, %v)", in, got, ok, want.want, want.ok)
		}
	}
}
