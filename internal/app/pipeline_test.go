package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/prateekpurohit13/grima/internal/bus"
	"github.com/prateekpurohit13/grima/internal/config"
	"github.com/prateekpurohit13/grima/internal/event"
	"github.com/prateekpurohit13/grima/internal/fingerprint"
	"github.com/prateekpurohit13/grima/internal/platform"
	"github.com/prateekpurohit13/grima/internal/sensor"
)

// countingSource is a sensor that exposes health counters.
type countingSource struct {
	name     string
	events   uint64
	errors   uint64
	dropped  uint64
	startErr error
}

func (s *countingSource) Name() string { return s.name }

func (s *countingSource) Start(ctx context.Context, out chan<- event.Event) error {
	return s.startErr
}

func (s *countingSource) Close() error { return nil }

func (s *countingSource) Stats() sensor.Stats {
	return sensor.Stats{Name: s.name, Events: s.events, Errors: s.errors, Dropped: s.dropped}
}

// quietSource is a sensor that only implements Source, so it has no counters.
type quietSource struct{ name string }

func (s *quietSource) Name() string                                            { return s.name }
func (s *quietSource) Start(ctx context.Context, out chan<- event.Event) error { return nil }
func (s *quietSource) Close() error                                            { return nil }

// healthPayload is the /healthz JSON as a consumer parses it.
type healthPayload struct {
	CalibrationReady bool    `json:"calibration_ready"`
	BusPublished     uint64  `json:"bus_published"`
	BusDropped       uint64  `json:"bus_dropped"`
	LiveProcesses    int     `json:"live_processes"`
	UptimeSeconds    float64 `json:"uptime_seconds"`
	Sensors          map[string]struct {
		Name      string            `json:"Name"`
		Events    uint64            `json:"Events"`
		Errors    uint64            `json:"Errors"`
		Dropped   uint64            `json:"Dropped"`
		Extra     map[string]uint64 `json:"Extra"`
		Reporting bool              `json:"Reporting"`
	} `json:"sensors"`
}

func parseHealth(t *testing.T, events *bus.Bus, sources []sensor.Source) healthPayload {
	t.Helper()

	snapshot := healthSnapshot(time.Now(), events, sources, fingerprint.NewEngine(config.Default()), nil)
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal health: %v", err)
	}

	var payload healthPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal health: %v\n%s", err, raw)
	}
	return payload
}

func testBus(t *testing.T) *bus.Bus {
	t.Helper()
	events := bus.New(64, bus.DropOldest)
	t.Cleanup(events.Close)
	return events
}

func TestHealthShowsCountersFromReportingSource(t *testing.T) {
	src := &countingSource{name: "procwatch", events: 42, errors: 2, dropped: 3}

	payload := parseHealth(t, testBus(t), []sensor.Source{src})

	seen, ok := payload.Sensors["procwatch"]
	if !ok {
		t.Fatalf("sensor missing from health output: %v", payload.Sensors)
	}
	if !seen.Reporting {
		t.Error("a source that exposes counters must be marked as reporting")
	}
	if seen.Events != src.events || seen.Errors != src.errors || seen.Dropped != src.dropped {
		t.Errorf("counters = events %d errors %d dropped %d, want %d/%d/%d",
			seen.Events, seen.Errors, seen.Dropped, src.events, src.errors, src.dropped)
	}
}

func TestHealthMarksSourceWithoutCounters(t *testing.T) {
	src := &quietSource{name: "no-counters"}

	payload := parseHealth(t, testBus(t), []sensor.Source{src})

	seen, ok := payload.Sensors["no-counters"]
	if !ok {
		t.Fatalf("sensor missing from health output: %v", payload.Sensors)
	}
	if seen.Reporting {
		t.Error("a source with no counters must not be marked as reporting")
	}
}

func TestHealthShowsReportingAndSilentSourcesTogether(t *testing.T) {
	reporting := &countingSource{name: "procwatch", events: 17}
	silent := &quietSource{name: "no-counters"}

	payload := parseHealth(t, testBus(t), []sensor.Source{reporting, silent})

	if len(payload.Sensors) != 2 {
		t.Fatalf("sensors = %v, want one entry per started source", payload.Sensors)
	}
	if got := payload.Sensors["procwatch"]; !got.Reporting || got.Events != 17 {
		t.Errorf("procwatch = %+v, want reporting with 17 events", got)
	}
	if got := payload.Sensors["no-counters"]; got.Reporting {
		t.Errorf("no-counters = %+v, want reporting false", got)
	}
}

// A sensor that failed to start must be absent from health, not present with a
// row of zeros that reads like a healthy idle sensor.
func TestSensorThatFailedToStartIsAbsentFromHealth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	good := &countingSource{name: "procwatch", events: 5}
	broken := &countingSource{name: "filewatch", startErr: errors.New("no permission")}
	events := testBus(t)
	host := platform.Set{OS: "test", Sources: []sensor.Source{good, broken}}

	started, err := startSensors(ctx, config.Default(), host, events, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("startSensors: %v", err)
	}
	if len(started) != 1 || started[0].Name() != "procwatch" {
		t.Fatalf("started = %v, want only procwatch", started)
	}

	payload := parseHealth(t, events, started)
	if _, ok := payload.Sensors["filewatch"]; ok {
		t.Errorf("failed sensor appears in health output: %v", payload.Sensors)
	}
	if got, ok := payload.Sensors["procwatch"]; !ok || !got.Reporting {
		t.Errorf("procwatch = %+v (present %v), want a reporting entry", got, ok)
	}
}

// Running with no sensors at all must fail loudly rather than report health
// while blind.
func TestAllSensorsFailingIsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	host := platform.Set{OS: "test", Sources: []sensor.Source{
		&countingSource{name: "filewatch", startErr: errors.New("no permission")},
	}}

	if _, err := startSensors(ctx, config.Default(), host, testBus(t), slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("startSensors succeeded with no usable sensor")
	}
}
