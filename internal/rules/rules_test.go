package rules

import (
	"testing"

	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
	"github.com/prateekpurohit13/grima/internal/score"
)

func TestRuleMatching(t *testing.T) {
	engine := NewEngine(config.Default())

	cases := []struct {
		name  string
		ev    event.Event
		want  string // empty means no rule should fire
		level score.Level
	}{
		{
			name:  "shadow copy deletion",
			ev:    event.Event{Kind: event.KindProcessStart, Cmdline: `vssadmin.exe delete shadows /all /quiet`},
			want:  "R-SHADOW-DELETE",
			level: score.LevelCritical,
		},
		{
			name:  "shadow copy deletion via wmic",
			ev:    event.Event{Kind: event.KindProcessStart, Cmdline: `wmic shadowcopy delete`},
			want:  "R-SHADOW-DELETE",
			level: score.LevelCritical,
		},
		{
			name:  "recovery disabled",
			ev:    event.Event{Kind: event.KindProcessStart, Cmdline: `bcdedit /set recoveryenabled no`},
			want:  "R-SHADOW-DELETE",
			level: score.LevelCritical,
		},
		{
			name:  "event log cleared",
			ev:    event.Event{Kind: event.KindProcessStart, Cmdline: `wevtutil cl System`},
			want:  "R-EVENTLOG-CLEAR",
			level: score.LevelCritical,
		},
		{
			name:  "usn journal deleted",
			ev:    event.Event{Kind: event.KindProcessStart, Cmdline: `fsutil usn deletejournal /D C:`},
			want:  "R-USN-DELETE",
			level: score.LevelHigh,
		},
		{
			name:  "backup process terminated",
			ev:    event.Event{Kind: event.KindProcessStart, Cmdline: `taskkill /IM sqlservr.exe /F`},
			want:  "R-BACKUP-KILL",
			level: score.LevelHigh,
		},
		{
			name:  "decoy touched",
			ev:    event.Event{Kind: event.KindDecoyTouch, Path: "/data/invoices_backup.pdf", DecoyID: "ab12"},
			want:  "R-DECOY-TOUCH",
			level: score.LevelCritical,
		},
		{
			name:  "persistence installed",
			ev:    event.Event{Kind: event.KindPersistenceInstall, Path: "/etc/cron.d/x", Persist: "cron"},
			want:  "R-PERSIST-INSTALL",
			level: score.LevelMedium,
		},

		// Sad paths: ordinary activity must not fire anything.
		{
			name: "benign listing",
			ev:   event.Event{Kind: event.KindProcessStart, Cmdline: `ls -la /data`},
		},
		{
			name: "benign file write",
			ev:   event.Event{Kind: event.KindFileWrite, Path: "/data/report.docx"},
		},
		{
			name: "terminator with no backup target",
			ev:   event.Event{Kind: event.KindProcessStart, Cmdline: `pkill nginx`},
		},
		{
			name: "shadow word in an unrelated argument",
			ev:   event.Event{Kind: event.KindProcessStart, Cmdline: `vim shadows.txt`},
		},
		{
			name: "command pattern on a file event",
			ev:   event.Event{Kind: event.KindFileWrite, Cmdline: `vssadmin delete shadows`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, hit := engine.Evaluate(tc.ev)
			if tc.want == "" {
				if hit {
					t.Fatalf("unexpected rule hit %q", got.ID)
				}
				return
			}
			if !hit {
				t.Fatalf("expected rule %q to fire", tc.want)
			}
			if got.ID != tc.want {
				t.Fatalf("rule = %q, want %q", got.ID, tc.want)
			}
			if got.Level != tc.level {
				t.Fatalf("severity = %v, want %v", got.Level, tc.level)
			}
			if got.Detail == "" {
				t.Fatal("a rule hit must carry a detail")
			}
		})
	}
}

// The highest-severity match wins when more than one rule matches.
func TestHighestSeverityWins(t *testing.T) {
	engine := NewEngine(config.Default())

	ev := event.Event{
		Kind:    event.KindPersistenceInstall,
		Path:    "/etc/cron.d/x",
		Persist: "cron",
	}

	got, hit := engine.Evaluate(ev)
	if !hit {
		t.Fatal("expected a hit")
	}
	if got.ID != "R-PERSIST-INSTALL" {
		t.Fatalf("rule = %q, want R-PERSIST-INSTALL", got.ID)
	}
}

func TestEmptyConfigDisablesPatternRules(t *testing.T) {
	cfg := config.Default()
	cfg.Rules = config.RulesConfig{Enabled: false}

	engine := NewEngine(cfg)
	if _, hit := engine.Evaluate(event.Event{
		Kind:    event.KindProcessStart,
		Cmdline: `vssadmin delete shadows /all`,
	}); hit {
		t.Fatal("pattern rules must not fire when their pattern list is empty")
	}
}
