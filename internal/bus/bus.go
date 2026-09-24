// Package bus is the bounded fan-out event queue that decouples sensors from
// consumers. Every drop is counted, never silent.
package bus

import (
	"sync"
	"sync/atomic"

	"github.com/prateekpurohit13/grima/internal/event"
)

// DropPolicy decides what happens when a queue is full.
type DropPolicy uint8

const (
	DropOldest DropPolicy = iota
	DropNewest
	Block
)

var policyNames = [...]string{"drop_oldest", "drop_newest", "block"}

func (p DropPolicy) String() string {
	if int(p) < len(policyNames) {
		return policyNames[p]
	}
	return "policy(?)"
}

// ParseDropPolicy resolves a configuration string to a policy.
func ParseDropPolicy(s string) (DropPolicy, bool) {
	for i, name := range policyNames {
		if name == s {
			return DropPolicy(i), true
		}
	}
	return DropOldest, false
}

// Stats is a snapshot of bus health.
type Stats struct {
	Published  uint64
	Dropped    uint64
	Delivered  map[string]uint64
	SubDropped map[string]uint64
}

type subscriber struct {
	name  string
	ch    chan event.Event
	count atomic.Uint64
	drops atomic.Uint64
}

func (s *subscriber) deliver(ev event.Event, policy DropPolicy) {
	select {
	case s.ch <- ev:
		s.count.Add(1)
		return
	default:
	}

	if policy == DropOldest {
		select {
		case <-s.ch:
			s.drops.Add(1)
		default:
		}
		select {
		case s.ch <- ev:
			s.count.Add(1)
			return
		default:
		}
	}
	s.drops.Add(1)
}

// Bus fans published events out to every subscriber.
type Bus struct {
	capacity int
	policy   DropPolicy

	in   chan event.Event
	done chan struct{}
	wg   sync.WaitGroup

	mu   sync.RWMutex
	subs []*subscriber

	seq       atomic.Uint64
	published atomic.Uint64
	dropped   atomic.Uint64
	closed    atomic.Bool

	closeOnce sync.Once
}

// New starts a bus with the given input capacity and drop policy.
func New(capacity int, policy DropPolicy) *Bus {
	if capacity <= 0 {
		capacity = 4096
	}
	b := &Bus{
		capacity: capacity,
		policy:   policy,
		in:       make(chan event.Event, capacity),
		done:     make(chan struct{}),
	}
	b.wg.Add(1)
	go b.fanout()
	return b
}

// Publish assigns a sequence number and enqueues the event. Never blocks unless
// the policy is Block.
func (b *Bus) Publish(ev event.Event) {
	if b.closed.Load() {
		b.dropped.Add(1)
		return
	}

	ev.Seq = b.seq.Add(1)
	b.published.Add(1)

	if b.policy == Block {
		select {
		case b.in <- ev:
		case <-b.done:
			b.dropped.Add(1)
		}
		return
	}

	select {
	case b.in <- ev:
		return
	default:
	}

	if b.policy == DropOldest {
		select {
		case <-b.in:
			b.dropped.Add(1)
		default:
		}
		select {
		case b.in <- ev:
			return
		default:
		}
	}
	b.dropped.Add(1)
}

// Subscribe registers a consumer. Each subscriber gets its own buffer, so a slow
// consumer does not stall the others.
func (b *Bus) Subscribe(name string, capacity int) <-chan event.Event {
	if capacity <= 0 {
		capacity = b.capacity
	}
	s := &subscriber{name: name, ch: make(chan event.Event, capacity)}

	b.mu.Lock()
	b.subs = append(b.subs, s)
	b.mu.Unlock()

	return s.ch
}

// Stats returns a snapshot of bus and per-subscriber health.
func (b *Bus) Stats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	st := Stats{
		Published:  b.published.Load(),
		Dropped:    b.dropped.Load(),
		Delivered:  make(map[string]uint64, len(b.subs)),
		SubDropped: make(map[string]uint64, len(b.subs)),
	}
	for _, s := range b.subs {
		st.Delivered[s.name] = s.count.Load()
		st.SubDropped[s.name] = s.drops.Load()
	}
	return st
}

// Close drains queued events to subscribers, then closes every subscriber
// channel. Idempotent.
func (b *Bus) Close() {
	b.closeOnce.Do(func() {
		b.closed.Store(true)
		close(b.done)
		b.wg.Wait()

		b.mu.Lock()
		for _, s := range b.subs {
			close(s.ch)
		}
		b.mu.Unlock()
	})
}

func (b *Bus) fanout() {
	defer b.wg.Done()

	for {
		select {
		case ev := <-b.in:
			b.deliverAll(ev)
		case <-b.done:
			// Drain what was already accepted so Close does not lose it.
			for {
				select {
				case ev := <-b.in:
					b.deliverAll(ev)
				default:
					return
				}
			}
		}
	}
}

func (b *Bus) deliverAll(ev event.Event) {
	b.mu.RLock()
	subs := b.subs
	b.mu.RUnlock()

	for _, s := range subs {
		s.deliver(ev, b.policy)
	}
}
