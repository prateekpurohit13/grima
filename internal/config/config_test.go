package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultsAreValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default configuration is invalid: %v", err)
	}
}

func TestLoadWithNoPathReturnsDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Bus.Capacity != Default().Bus.Capacity {
		t.Fatal("expected the default capacity")
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grima.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadOverridesDefaults(t *testing.T) {
	path := writeConfig(t, `
[bus]
capacity = 128
drop_policy = "drop_newest"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Bus.Capacity != 128 {
		t.Fatalf("capacity = %d, want 128", cfg.Bus.Capacity)
	}
	if cfg.Bus.DropPolicy != "drop_newest" {
		t.Fatalf("drop_policy = %q", cfg.Bus.DropPolicy)
	}
}

// Sad path: a typo must surface at startup, not silently disable a setting.
func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := writeConfig(t, `
[bus]
capcity = 128
`)

	if _, err := Load(path); err == nil {
		t.Fatal("an unknown key should be rejected")
	}
}

func TestLoadRejectsMalformedTOML(t *testing.T) {
	if _, err := Load(writeConfig(t, "this is not toml ===")); err == nil {
		t.Fatal("malformed TOML should be rejected")
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.toml")); err == nil {
		t.Fatal("a missing config file should be an error")
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	cases := map[string]func(*Config){
		"zero bus capacity": func(c *Config) { c.Bus.Capacity = 0 },
		"unknown drop policy": func(c *Config) {
			c.Bus.DropPolicy = "drop_sideways"
		},
		"zero entropy sample": func(c *Config) { c.FileWatch.EntropySampleBytes = 0 },
		"zero ngram length":   func(c *Config) { c.Window.NGramLength = 0 },
		"zero decay":          func(c *Config) { c.Window.DecayHalfLife = 0 },
		"zero sigma floor":    func(c *Config) { c.Calibration.EntropySigmaFloor = 0 },
		"zero min samples":    func(c *Config) { c.Calibration.MinSamples = 0 },
		"unknown alert level": func(c *Config) { c.Response.AlertMinLevel = "loud" },
		"unknown suspend level": func(c *Config) {
			c.Response.SuspendMinLevel = "loud"
		},
		"relative monitor path": func(c *Config) {
			c.General.MonitorPaths = []string{"relative/dir"}
		},
		"no monitor paths": func(c *Config) { c.General.MonitorPaths = nil },
		"negative weight": func(c *Config) {
			c.Scoring.Weights = []Weight{{Name: "entropy_deviation", Weight: -1}}
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

// Sad path: suspension below critical would let a heuristic score stop a
// legitimate process, so the combination is rejected outright.
func TestValidateRejectsSuspendBelowCritical(t *testing.T) {
	cfg := Default()
	cfg.Response.EnableSuspend = true
	cfg.Response.SuspendMinLevel = "high"

	if err := cfg.Validate(); err == nil {
		t.Fatal("enabling suspend below critical should be rejected")
	}
}

func TestDurationUnmarshalRejectsGarbage(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText([]byte("soon")); err == nil {
		t.Fatal("a non-duration string should be rejected")
	}
	if err := d.UnmarshalText([]byte("90s")); err != nil {
		t.Fatalf("valid duration rejected: %v", err)
	}
	if d.Std().Seconds() != 90 {
		t.Fatalf("duration = %v, want 90s", d.Std())
	}
}

func TestWeightFor(t *testing.T) {
	cfg := Default()

	if got := cfg.WeightFor("entropy_deviation"); got != 1.0 {
		t.Fatalf("weight = %v, want 1.0", got)
	}
	if got := cfg.WeightFor("no_such_signal"); got != 0 {
		t.Fatalf("weight = %v, want 0 for an unknown signal", got)
	}
}

func TestAttributionDefaultsAreCorrelation(t *testing.T) {
	cfg := Default()
	if cfg.Attribution.Mode != AttributionCorrelate {
		t.Fatalf("mode = %q, want %q so nothing regresses unelevated", cfg.Attribution.Mode, AttributionCorrelate)
	}
	if cfg.Attribution.MaxDelay.Std() <= 0 {
		t.Fatalf("max_delay = %s, want a positive wait", cfg.Attribution.MaxDelay.Std())
	}
}

func TestAttributionModes(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		wantErr bool
	}{
		{"default", AttributionCorrelate, false},
		{"audit", AttributionAudit, false},
		{"empty means correlate", "", false},
		{"etw is named but not implemented", AttributionETW, true},
		{"typo", "correlte", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Attribution.Mode = tc.mode
			err := cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("mode %q was accepted", tc.mode)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("mode %q was rejected: %v", tc.mode, err)
			}
		})
	}
}

// Sad path: a delay of zero would hold a file event forever, or not at all.
func TestAttributionMaxDelayMustBePositive(t *testing.T) {
	cfg := Default()
	cfg.Attribution.MaxDelay = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("want an error for a non-positive max_delay")
	}
}

func TestLoadAttributionSection(t *testing.T) {
	path := writeConfig(t, `
[general]
monitor_paths = ["C:\\data"]

[attribution]
mode = "audit"
max_delay = "150ms"
audit_setup = false
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Attribution.Mode != AttributionAudit {
		t.Fatalf("mode = %q", cfg.Attribution.Mode)
	}
	if cfg.Attribution.MaxDelay.Std() != 150*time.Millisecond {
		t.Fatalf("max_delay = %s", cfg.Attribution.MaxDelay.Std())
	}
	if cfg.Attribution.AuditSetup {
		t.Fatal("audit_setup = true, want the configured false")
	}
}
