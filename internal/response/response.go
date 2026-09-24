// Package response applies actions to verdicts.
//
// The default is Observe. Terminating or suspending a process on a heuristic
// score is a denial-of-service primitive: a false positive against a database
// process is worse than a missed detection that alerts. Suspension is opt-in and
// gated on Critical.
package response

import (
	"fmt"
	"log/slog"

	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/score"
)

// Action is what the handler does with a verdict.
type Action uint8

const (
	// Observe records the verdict with no side effect. Default.
	Observe Action = iota
	// Alert emits the verdict to the configured sink.
	Alert
	// Suspend stops the process from being scheduled.
	Suspend
)

var actionNames = [...]string{"observe", "alert", "suspend"}

// String returns the stable lowercase name of the action.
func (a Action) String() string {
	if int(a) < len(actionNames) {
		return actionNames[a]
	}
	return "action(?)"
}

// Handler applies response policy to verdicts.
type Handler struct {
	cfg        config.Config
	alertMin   score.Level
	suspendMin score.Level
	log        *slog.Logger
}

// NewHandler builds a handler from configuration.
func NewHandler(cfg config.Config, log *slog.Logger) *Handler {
	alertMin, _ := score.ParseLevel(cfg.Response.AlertMinLevel)
	suspendMin, _ := score.ParseLevel(cfg.Response.SuspendMinLevel)

	return &Handler{
		cfg:        cfg,
		alertMin:   alertMin,
		suspendMin: suspendMin,
		log:        log,
	}
}

// Decide returns the action this verdict warrants, without performing it.
func (h *Handler) Decide(v score.Verdict) Action {
	if h.cfg.Response.EnableSuspend && v.Level >= h.suspendMin && v.Level >= score.LevelCritical {
		return Suspend
	}
	if v.Level >= h.alertMin {
		return Alert
	}
	return Observe
}

// Apply performs the action for a verdict.
func (h *Handler) Apply(v score.Verdict) error {
	switch h.Decide(v) {
	case Observe:
		return nil

	case Alert:
		h.log.Warn("ransomware risk detected",
			"pid", v.PID,
			"process", v.ProcName,
			"score", fmt.Sprintf("%.1f", v.Score),
			"level", v.Level.String(),
			"override", v.Override,
			"signals", signalSummary(v),
		)
		return nil

	case Suspend:
		if err := suspendProcess(v.PID); err != nil {
			return fmt.Errorf("suspend pid %d: %w", v.PID, err)
		}
		h.log.Warn("process suspended",
			"pid", v.PID,
			"process", v.ProcName,
			"score", fmt.Sprintf("%.1f", v.Score),
			"level", v.Level.String(),
			"signals", signalSummary(v),
		)
		return nil
	}
	return nil
}

func signalSummary(v score.Verdict) string {
	if len(v.Signals) == 0 {
		return "none"
	}
	out := ""
	for i, s := range v.Signals {
		if i > 0 {
			out += "; "
		}
		out += s.Name
		if i >= 4 {
			out += "; ..."
			break
		}
	}
	return out
}
