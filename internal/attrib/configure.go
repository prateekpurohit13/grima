package attrib

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/prateekpurohit13/grima/internal/config"
)

// Attribution modes. They are the values of config.AttributionConfig.Mode.
const (
	modeHost      = config.AttributionHost
	modeCorrelate = config.AttributionCorrelate
	modeAudit     = config.AttributionAudit
)

// defaultMaxDelay is how long a file event waits for a causal record when the
// configuration does not say.
const defaultMaxDelay = 500 * time.Millisecond

// Configure attaches the causal source named by the configuration.
//
// Every failure is reported and leaves correlation in place. Naming the actor is
// an upgrade, so a mechanism that cannot start must degrade coverage and say so,
// never stop the detector or leave it silently blind.
func (a *Attributor) Configure(cfg config.AttributionConfig, paths []string, log *slog.Logger) {
	if a == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	a.log = log
	a.mode = cfg.Mode
	if a.mode == "" {
		a.mode = modeHost
	}

	// Host mode makes no per-process claim: every event is filed against the
	// host fingerprint. That is the honest default, because correlation was
	// measured at 0% and filing cumulative evidence against a guess is what
	// fragments a slow drip across unrelated processes.
	if a.mode == modeHost {
		log.Info("attribution mode", "mode", modeHost)
		return
	}

	if a.mode == modeCorrelate {
		log.Info("attribution mode", "mode", modeCorrelate)
		return
	}

	delay := resolveDelay(cfg)

	src, err := newSource(a.mode, sourceOptions{paths: paths, setup: cfg.AuditSetup, log: log})
	if err != nil {
		log.Warn("causal attribution unavailable, correlating instead", "mode", a.mode, "reason", err)
		a.mode = modeCorrelate
		return
	}

	if err := a.attachSource(src, delay); err != nil {
		log.Warn("causal attribution unavailable, correlating instead", "mode", a.mode, "reason", err)
		a.mode = modeCorrelate
		return
	}

	log.Info("causal attribution active",
		"mode", a.mode,
		"source", src.Describe(),
		"max_delay", delay,
		"paths", len(paths))
}

// attachSource starts a causal source and the resolver that reads it.
func (a *Attributor) attachSource(src Source, delay time.Duration) error {
	if a.log == nil {
		a.log = slog.Default()
	}

	index := newCausalIndex(causalWindow(delay), int32(os.Getpid()))
	if err := src.Start(index.record); err != nil {
		_ = src.Close()
		return err
	}

	a.causal = index
	a.source = src
	a.delay = delay
	a.pending = startPendingQueue(a)
	return nil
}

// resolveDelay returns the configured delay, or the default when the
// configuration does not carry a usable one.
func resolveDelay(cfg config.AttributionConfig) time.Duration {
	delay := cfg.MaxDelay.Std()
	if delay <= 0 {
		return defaultMaxDelay
	}
	return delay
}

// causalWindow is how long a causal record stays usable.
//
// It must exceed the wait, because the record has to survive from the write to
// the file event that asks about it. It must not grow without bound: a window
// long enough to span two workloads lets a STALE record satisfy a late event, so
// the blame lands on whoever wrote that path last time. Measured — at
// max_delay 1s the window was 4s and accuracy fell to 84.6% with 88 causal
// decisions wrong, while every setting whose window stayed at 2s held 100%
// causal correctness.
func causalWindow(delay time.Duration) time.Duration {
	window := delay + causalWindowMargin
	if window < 2*time.Second {
		window = 2 * time.Second
	}
	return window
}

// causalWindowMargin is the slack a record needs beyond the wait it may serve,
// covering the gap between the write and the event that reports it.
const causalWindowMargin = 250 * time.Millisecond

// sourceOptions are what a causal source needs to start.
type sourceOptions struct {
	paths []string
	setup bool
	log   *slog.Logger
}

// unknownModeError reports a mode the platform cannot run.
func unknownModeError(mode string) error {
	return fmt.Errorf("attribution mode %q is not supported on this platform", mode)
}
