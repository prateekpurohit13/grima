package procwatch

import (
	"context"
	"log/slog"
	"testing"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/prateekpurohit13/grima/internal/attrib"
	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
)

// benchChannel is deep enough that a benchmark never fills it, so a passing
// channel send costs the same as it does in the loop.
const benchChannel = 1 << 16

func benchmarkSource() *Source {
	cfg := config.Default()
	return New(cfg, attrib.New(cfg.Window.DecayHalfLife.Std()), slog.New(slog.DiscardHandler))
}

// BenchmarkOneSamplePass times a pass over a process table the sensor has never
// seen: every process is new, so name, parent, executable and command line are
// read and a start event is emitted for each.
func BenchmarkOneSamplePass(b *testing.B) {
	ctx := context.Background()
	out := make(chan event.Event, benchChannel)

	b.ReportAllocs()
	for b.Loop() {
		s := benchmarkSource()
		s.sample(ctx, out)
	}
}

// BenchmarkOneSamplePassKnownProcesses times the steady state: the table is
// already known, so the work is the scan plus a write-counter read and a map
// update per process. This is the cost the default interval pays.
func BenchmarkOneSamplePassKnownProcesses(b *testing.B) {
	ctx := context.Background()
	out := make(chan event.Event, benchChannel)
	s := benchmarkSource()
	s.sample(ctx, out)

	b.ReportAllocs()
	for b.Loop() {
		s.sample(ctx, out)
	}
}

// BenchmarkProcessTableScan times the process list call on its own, so the cost
// of the table scan can be told apart from the per-process work.
func BenchmarkProcessTableScan(b *testing.B) {
	ctx := context.Background()

	for b.Loop() {
		if _, err := process.ProcessesWithContext(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWriteCounterRead times reading the write counter for every process,
// which is the per-process work every sample pays.
func BenchmarkWriteCounterRead(b *testing.B) {
	ctx := context.Background()
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		for _, p := range procs {
			if counters, err := p.IOCounters(); err == nil && counters != nil {
				_ = counters.WriteBytes
			}
		}
	}
}

// BenchmarkProcessFieldReads reports how often the descriptive fields can be
// read on this host. A field that cannot be read is empty in the event, which
// bounds how much attribution may rely on it.
func BenchmarkProcessFieldReads(b *testing.B) {
	ctx := context.Background()

	var processes, nameErrors, parentErrors uint64
	var exeErrors, exeEmpty, cmdlineErrors, cmdlineEmpty uint64

	for b.Loop() {
		procs, err := process.ProcessesWithContext(ctx)
		if err != nil {
			b.Fatal(err)
		}
		for _, p := range procs {
			processes++
			if _, err := p.Name(); err != nil {
				nameErrors++
			}
			if _, err := p.Ppid(); err != nil {
				parentErrors++
			}
			exe, err := p.Exe()
			if err != nil {
				exeErrors++
			}
			if exe == "" {
				exeEmpty++
			}
			cmdline, err := p.Cmdline()
			if err != nil {
				cmdlineErrors++
			}
			if cmdline == "" {
				cmdlineEmpty++
			}
		}
	}

	runs := float64(b.N)
	b.ReportMetric(float64(processes)/runs, "processes/sample")
	b.ReportMetric(float64(nameErrors)/runs, "name_errors/sample")
	b.ReportMetric(float64(parentErrors)/runs, "ppid_errors/sample")
	b.ReportMetric(float64(exeErrors)/runs, "exe_errors/sample")
	b.ReportMetric(float64(exeEmpty)/runs, "exe_empty/sample")
	b.ReportMetric(float64(cmdlineErrors)/runs, "cmdline_errors/sample")
	b.ReportMetric(float64(cmdlineEmpty)/runs, "cmdline_empty/sample")
}
