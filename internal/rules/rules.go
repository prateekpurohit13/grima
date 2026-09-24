// Package rules implements the hard-rule layer.
//
// Rules are pure predicates over a single event: no cross-event state, no
// window dependency. That keeps them instantaneous — they subscribe to the bus
// directly and are evaluated on arrival rather than at the next scoring tick —
// and it makes each one trivially testable.
//
// A rule hit becomes an Override, which sets a floor on the risk level and is
// never averaged away by benign signals.
package rules

import (
	"fmt"
	"strings"

	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
	"github.com/prateekpurohit13/grima/internal/score"
)

// Rule is one hard detection rule.
type Rule struct {
	ID          string
	Description string
	Severity    score.Level
	Match       func(event.Event) bool
}

// Engine evaluates the rule set against single events.
type Engine struct {
	rules []Rule
}

// NewEngine builds the rule set from configuration.
//
// Detection logic for the anti-recovery rules is derived from published rule
// corpora; attribution is recorded in THIRD_PARTY_NOTICES.md.
func NewEngine(cfg config.Config) *Engine {
	rc := cfg.Rules

	e := &Engine{}
	e.rules = []Rule{
		{
			ID:          "R-DECOY-TOUCH",
			Description: "decoy file written, renamed, or deleted",
			Severity:    score.LevelCritical,
			Match: func(ev event.Event) bool {
				return ev.Kind == event.KindDecoyTouch
			},
		},
		{
			ID:          "R-PERSIST-INSTALL",
			Description: "new persistence mechanism installed",
			Severity:    score.LevelMedium,
			Match: func(ev event.Event) bool {
				return ev.Kind == event.KindPersistenceInstall
			},
		},
		{
			ID:          "R-SHADOW-DELETE",
			Description: "shadow copy or recovery catalog deletion",
			Severity:    score.LevelCritical,
			Match: func(ev event.Event) bool {
				if ev.Kind != event.KindProcessStart {
					return false
				}
				_, ok := containsAny(ev.Cmdline, rc.ShadowCommands)
				return ok
			},
		},
		{
			ID:          "R-EVENTLOG-CLEAR",
			Description: "event log cleared",
			Severity:    score.LevelCritical,
			Match: func(ev event.Event) bool {
				if ev.Kind != event.KindProcessStart {
					return false
				}
				_, ok := containsAny(ev.Cmdline, rc.EventLogCommands)
				return ok
			},
		},
		{
			ID:          "R-USN-DELETE",
			Description: "USN journal deleted",
			Severity:    score.LevelHigh,
			Match: func(ev event.Event) bool {
				if ev.Kind != event.KindProcessStart {
					return false
				}
				_, ok := containsAny(ev.Cmdline, rc.USNCommands)
				return ok
			},
		},
		{
			ID:          "R-BACKUP-KILL",
			Description: "backup or database process terminated",
			Severity:    score.LevelHigh,
			Match: func(ev event.Event) bool {
				if ev.Kind != event.KindProcessStart {
					return false
				}
				_, terminated := containsAny(ev.Cmdline, rc.Terminators)
				if !terminated {
					return false
				}
				_, target := containsAny(ev.Cmdline, rc.BackupProcesses)
				return target
			},
		},
	}
	return e
}

// Rules returns the rule set for documentation and display.
func (e *Engine) Rules() []Rule { return e.rules }

// Evaluate returns the highest-severity rule hit for an event, if any.
func (e *Engine) Evaluate(ev event.Event) (score.Override, bool) {
	var (
		best    score.Override
		matched bool
	)
	for _, r := range e.rules {
		if !r.Match(ev) {
			continue
		}
		if matched && r.Severity <= best.Level {
			continue
		}
		best = score.Override{
			ID:     r.ID,
			Level:  r.Severity,
			Detail: fmt.Sprintf("%s: %s", r.Description, summarise(ev)),
		}
		matched = true
	}
	return best, matched
}

func summarise(ev event.Event) string {
	switch {
	case ev.Kind == event.KindDecoyTouch:
		return fmt.Sprintf("decoy %s touched at %s", ev.DecoyID, ev.Path)
	case ev.Kind == event.KindPersistenceInstall:
		return fmt.Sprintf("%s entry installed at %s", ev.Persist, ev.Path)
	case ev.Cmdline != "":
		return truncate(ev.Cmdline, 160)
	case ev.Path != "":
		return ev.Path
	default:
		return ev.ProcName
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// containsAny reports whether haystack contains any needle, case-insensitively.
func containsAny(haystack string, needles []string) (string, bool) {
	if haystack == "" || len(needles) == 0 {
		return "", false
	}
	lower := strings.ToLower(haystack)
	for _, n := range needles {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(n)) {
			return n, true
		}
	}
	return "", false
}
