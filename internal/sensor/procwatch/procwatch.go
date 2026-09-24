// Package procwatch tracks process creation and exit, and samples per-process
// write volume to feed the attributor that links file changes to processes.
package procwatch

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/prateekpurohit13/grima/internal/attrib"
	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
	"github.com/prateekpurohit13/grima/internal/sensor"
)

const name = "procwatch"

type procInfo struct {
	name    string
	ppid    int32
	exe     string
	cmdline string
}

// Source samples the process table.
type Source struct {
	cfg    config.Config
	log    *slog.Logger
	attrib *attrib.Attributor

	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	known      map[int32]procInfo
	writeBytes map[int32]uint64

	events  atomic.Uint64
	errors  atomic.Uint64
	dropped atomic.Uint64
	// tracked mirrors len(known) so Stats can be read from another goroutine
	// without touching the map, which only the sample loop may write.
	tracked atomic.Int64
}

// New returns a process sensor.
func New(cfg config.Config, at *attrib.Attributor, log *slog.Logger) *Source {
	return &Source{
		cfg:        cfg,
		log:        log,
		attrib:     at,
		done:       make(chan struct{}),
		known:      make(map[int32]procInfo),
		writeBytes: make(map[int32]uint64),
	}
}

func (s *Source) Name() string { return name }

// Start begins sampling. It returns an error only if the process table cannot be
// read at all.
func (s *Source) Start(ctx context.Context, out chan<- event.Event) error {
	if _, err := process.ProcessesWithContext(ctx); err != nil {
		return err
	}

	s.wg.Add(1)
	go s.loop(ctx, out)
	return nil
}

// Close stops sampling. Idempotent.
func (s *Source) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.wg.Wait()
	})
	return nil
}

// Stats reports sensor health.
func (s *Source) Stats() sensor.Stats {
	return sensor.Stats{
		Name:    name,
		Events:  s.events.Load(),
		Errors:  s.errors.Load(),
		Dropped: s.dropped.Load(),
		Extra:   map[string]uint64{"tracked": uint64(s.tracked.Load())},
	}
}

func (s *Source) loop(ctx context.Context, out chan<- event.Event) {
	defer s.wg.Done()

	interval := s.cfg.ProcWatch.SampleInterval.Std()
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.sample(ctx, out)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case <-ticker.C:
			s.sample(ctx, out)
		}
	}
}

func (s *Source) sample(ctx context.Context, out chan<- event.Event) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		s.errors.Add(1)
		s.log.Warn("process scan failed", "source", name, "error", err)
		return
	}

	now := time.Now()
	alive := make(map[int32]struct{}, len(procs))

	for _, p := range procs {
		pid := p.Pid
		alive[pid] = struct{}{}

		if _, known := s.known[pid]; !known {
			info := describe(p)
			s.known[pid] = info
			s.emit(out, event.Event{
				Kind:             event.KindProcessStart,
				Time:             now,
				PID:              pid,
				PPID:             info.ppid,
				ProcName:         info.name,
				Exe:              info.exe,
				Cmdline:          info.cmdline,
				AttribConfidence: 1,
			})
		}

		s.observeWrites(pid, p)
	}

	for pid, info := range s.known {
		if _, ok := alive[pid]; ok {
			continue
		}
		delete(s.known, pid)
		delete(s.writeBytes, pid)
		s.attrib.Forget(pid)
		s.emit(out, event.Event{
			Kind:             event.KindProcessExit,
			Time:             now,
			PID:              pid,
			PPID:             info.ppid,
			ProcName:         info.name,
			AttribConfidence: 1,
		})
	}

	s.tracked.Store(int64(len(s.known)))
}

// observeWrites records the write-volume delta so file events can be blamed on a
// process by correlating timing with volume.
func (s *Source) observeWrites(pid int32, p *process.Process) {
	counters, err := p.IOCounters()
	if err != nil || counters == nil {
		return
	}

	previous, seen := s.writeBytes[pid]
	s.writeBytes[pid] = counters.WriteBytes
	if !seen || counters.WriteBytes <= previous {
		return
	}

	info := s.known[pid]
	s.attrib.Observe(pid, info.name, info.ppid, info.exe, counters.WriteBytes-previous)
}

func (s *Source) emit(out chan<- event.Event, ev event.Event) {
	ev.Source = name
	s.events.Add(1)

	select {
	case out <- ev:
	default:
		s.dropped.Add(1)
	}
}

func describe(p *process.Process) procInfo {
	info := procInfo{}
	if n, err := p.Name(); err == nil {
		info.name = n
	}
	if ppid, err := p.Ppid(); err == nil {
		info.ppid = ppid
	}
	if exe, err := p.Exe(); err == nil {
		info.exe = exe
	}
	if cmd, err := p.Cmdline(); err == nil {
		info.cmdline = cmd
	}
	return info
}
