// Package platform reports the host OS and builds the matching sensor set.
package platform

import (
	"log/slog"
	"runtime"
	"time"

	"github.com/prateekpurohit13/grima/internal/attrib"
	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/decoy"
	"github.com/prateekpurohit13/grima/internal/sensor"
	"github.com/prateekpurohit13/grima/internal/sensor/filewatch"
	"github.com/prateekpurohit13/grima/internal/sensor/persistwatch"
	"github.com/prateekpurohit13/grima/internal/sensor/procwatch"
)

// Set is the sensor set for one host.
type Set struct {
	OS      string
	Sources []sensor.Source
	Attrib  *attrib.Attributor
	Decoys  *decoy.Registry
}

// Detect builds the sensor set for the host OS. Everything above the sensors is
// platform independent, so this is a factory and nothing more.
func Detect(cfg config.Config, log *slog.Logger) Set {
	sample := cfg.ProcWatch.SampleInterval.Std()
	if sample <= 0 {
		sample = time.Second
	}
	// Two sample ticks: wide enough to catch a write that landed between ticks,
	// narrow enough not to blame a process that stopped writing a while ago.
	at := attrib.New(2 * sample)
	decoys := decoy.NewRegistry()

	return Set{
		OS:     runtime.GOOS,
		Attrib: at,
		Decoys: decoys,
		Sources: []sensor.Source{
			filewatch.New(cfg, at, decoys, log),
			procwatch.New(cfg, at, log),
			persistwatch.New(cfg, log),
		},
	}
}
