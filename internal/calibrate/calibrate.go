// Package calibrate captures, persists, and reloads the host baseline that
// deviation-based signals are measured against.
//
// There is no training step and no model artifact: a baseline is a set of
// distributions measured on the host it will be used on. Until one exists,
// GRIMA runs in uncalibrated mode and deviation signals are suppressed.
package calibrate

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prateekpurohit13/grima/internal/bus"
	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
)

// Version is the baseline schema version. A baseline whose version does not
// match is ignored rather than fatal.
const Version = 1

// Dist is a measured distribution.
type Dist struct {
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"std_dev"`
	N      int     `json:"n"`
}

// Baseline is the host-specific measurement set.
type Baseline struct {
	Version         int                `json:"version"`
	Host            string             `json:"host"`
	CapturedAt      time.Time          `json:"captured_at"`
	WarmupSeconds   float64            `json:"warmup_seconds"`
	MinSamples      int                `json:"min_samples"`
	SigmaFloor      float64            `json:"sigma_floor"`
	EntropyByExt    map[string]Dist    `json:"entropy_by_ext"`
	WriteRateByProc map[string]float64 `json:"write_rate_by_proc"`
	HostEventRate   Dist               `json:"host_event_rate"`
	DirFanout       Dist               `json:"dir_fanout"`
	KnownExt        []string           `json:"known_ext"`
}

// Ready reports whether the baseline has enough samples for deviation signals to
// be meaningful. A baseline that is not ready puts GRIMA in uncalibrated mode.
func (b *Baseline) Ready() bool {
	if b == nil {
		return false
	}
	total := 0
	for _, d := range b.EntropyByExt {
		total += d.N
	}
	return total >= b.MinSamples
}

// Sigma returns the standard deviation to use for an extension, applying the
// configured floor so that a degenerate distribution never divides by zero.
func (b *Baseline) Sigma(ext string) (Dist, bool) {
	d, ok := b.EntropyByExt[ext]
	if !ok || d.N < 2 {
		return Dist{}, false
	}
	if d.StdDev < b.SigmaFloor {
		d.StdDev = b.SigmaFloor
	}
	return d, true
}

// KnowsExt reports whether the host had seen this extension before calibration.
// Renames to an extension the host has never seen are a primary signal.
func (b *Baseline) KnowsExt(ext string) bool {
	if b == nil || ext == "" {
		return true
	}
	for _, e := range b.KnownExt {
		if e == ext {
			return true
		}
	}
	return false
}

// Capture observes the host for the configured warm-up period and returns the
// measured baseline.
func Capture(ctx context.Context, cfg config.Config, b *bus.Bus) (*Baseline, error) {
	ch := b.Subscribe("calibration", 16384)

	entropyByExt := make(map[string][]float64)
	writeCounts := make(map[string]int)
	dirSets := make(map[string]map[string]struct{})
	perSecond := make(map[int64]int)
	totalEvents := 0
	extSeen := make(map[string]struct{})

	start := time.Now()
	warmup := cfg.Calibration.Warmup.Std()
	timer := time.NewTimer(warmup)
	defer timer.Stop()

collect:
	for {
		select {
		case <-ctx.Done():
			break collect
		case <-timer.C:
			break collect
		case ev, ok := <-ch:
			if !ok {
				break collect
			}
			totalEvents++
			perSecond[ev.Time.Unix()]++

			if ev.Extension() != "" {
				extSeen[ev.Extension()] = struct{}{}
			}
			if ev.Kind != event.KindFileWrite {
				continue
			}
			if ev.Entropy > 0 && ev.Extension() != "" {
				entropyByExt[ev.Extension()] = append(entropyByExt[ev.Extension()], ev.Entropy)
			}
			if ev.PID == 0 || ev.ProcName == "" {
				continue
			}
			writeCounts[ev.ProcName]++
			dirs := dirSets[ev.ProcName]
			if dirs == nil {
				dirs = make(map[string]struct{})
				dirSets[ev.ProcName] = dirs
			}
			if d := event.ParentDir(ev.Path); d != "" {
				dirs[d] = struct{}{}
			}
		}
	}

	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = warmup.Seconds()
	}

	bl := &Baseline{
		Version:         Version,
		CapturedAt:      time.Now(),
		WarmupSeconds:   elapsed,
		MinSamples:      cfg.Calibration.MinSamples,
		SigmaFloor:      cfg.Calibration.EntropySigmaFloor,
		EntropyByExt:    make(map[string]Dist, len(entropyByExt)),
		WriteRateByProc: make(map[string]float64, len(writeCounts)),
	}
	if host, err := os.Hostname(); err == nil {
		bl.Host = host
	}

	for ext, samples := range entropyByExt {
		mean, sd := meanStdDev(samples)
		bl.EntropyByExt[ext] = Dist{Mean: mean, StdDev: sd, N: len(samples)}
	}

	for name, n := range writeCounts {
		bl.WriteRateByProc[name] = float64(n) / elapsed
	}

	fanouts := make([]float64, 0, len(dirSets))
	for _, dirs := range dirSets {
		fanouts = append(fanouts, float64(len(dirs)))
	}
	mean, sd := meanStdDev(fanouts)
	bl.DirFanout = Dist{Mean: mean, StdDev: sd, N: len(fanouts)}

	rates := make([]float64, 0, len(perSecond))
	for _, n := range perSecond {
		rates = append(rates, float64(n))
	}
	mean, sd = meanStdDev(rates)
	bl.HostEventRate = Dist{Mean: mean, StdDev: sd, N: len(rates)}

	for ext := range extSeen {
		bl.KnownExt = append(bl.KnownExt, ext)
	}
	// Extensions that already exist under the monitored paths are "known" even
	// if nothing wrote to them during the warm-up.
	for _, ext := range scanExtensions(cfg.General.MonitorPaths) {
		if _, ok := extSeen[ext]; !ok {
			bl.KnownExt = append(bl.KnownExt, ext)
		}
	}

	return bl, nil
}

// Save writes the baseline as JSON.
func (b *Baseline) Save(path string) error {
	if path == "" {
		return fmt.Errorf("calibration.baseline_path is empty")
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("encode baseline: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write baseline %s: %w", path, err)
	}
	return nil
}

// Load reads a baseline. A missing, unreadable, or version-mismatched file
// yields (nil, nil): the caller falls back to uncalibrated mode rather than
// refusing to start.
func Load(path string) (*Baseline, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read baseline %s: %w", path, err)
	}

	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, nil
	}
	if b.Version != Version {
		return nil, nil
	}
	return &b, nil
}

func meanStdDev(xs []float64) (float64, float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(len(xs))

	if len(xs) < 2 {
		return mean, 0
	}
	var sq float64
	for _, x := range xs {
		d := x - mean
		sq += d * d
	}
	return mean, math.Sqrt(sq / float64(len(xs)-1))
}

// scanExtensions walks the monitored paths, bounded in both depth and entry
// count, and returns the set of extensions present.
func scanExtensions(roots []string) []string {
	const (
		maxEntries = 20000
		maxDepth   = 4
	)

	seen := make(map[string]struct{})
	entries := 0

	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxDepth || entries >= maxEntries {
			return
		}
		items, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, it := range items {
			if entries >= maxEntries {
				return
			}
			entries++
			name := it.Name()
			if it.IsDir() {
				if strings.HasPrefix(name, ".") {
					continue
				}
				walk(filepath.Join(dir, name), depth+1)
				continue
			}
			if ext := event.Extension(name); ext != "" {
				seen[ext] = struct{}{}
			}
		}
	}

	for _, root := range roots {
		walk(root, 0)
	}

	out := make([]string, 0, len(seen))
	for ext := range seen {
		out = append(out, ext)
	}
	return out
}
