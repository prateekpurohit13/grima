# GRIMA: Design & Interface Reference

This is the authoritative design document for GRIMA. It defines the frozen interfaces,
signal semantics, calibration model, fusion rules, and configuration surface. Where this
document and an implementation disagree, this document is the specification and the
implementation is the bug.

---

## Table of Contents

1. [Concepts](#1-concepts)
2. [The Event Contract (Frozen)](#2-the-event-contract-frozen)
3. [The Source Interface](#3-the-source-interface)
4. [The Event Bus](#4-the-event-bus)
5. [Signal Reference](#5-signal-reference)
6. [Fingerprint Engine Reference](#6-fingerprint-engine-reference)
7. [Calibration Reference](#7-calibration-reference)
8. [Scoring & Fusion Reference](#8-scoring--fusion-reference)
9. [Rule Reference](#9-rule-reference)
10. [Response Reference](#10-response-reference)
11. [Configuration Reference](#11-configuration-reference)
12. [Observability & Health](#12-observability--health)
13. [Error Handling](#13-error-handling)
14. [Testing Contract](#14-testing-contract)
15. [Non-Goals](#15-non-goals)

Architecture & layer mechanics: **`docs/architecture.md`**. Goals, roadmap, risks:
**`docs/plan.md`**.

---

## 1. Concepts

| Term | Meaning |
|---|---|
| **Event** | One observable system action: a file write, a process start, a persistence install. The single currency of the pipeline. |
| **Source** | A sensor that produces events. One implementation per OS capability. |
| **Bus** | Bounded, fan-out event queue with an explicit drop policy and drop counter. |
| **Window** | The decaying half of a process fingerprint: bounded counts over recent events. |
| **Cumulative counter** | The non-decaying half: totals of irreversible actions since process start. |
| **Fingerprint** | One process's `Window` plus its non-decaying cumulative counters. |
| **Tree vector** | The sum of fingerprints across an ancestor/process group. This is what gets scored. |
| **Baseline** | Host-specific measured distributions (entropy per extension, write-rate EWMA, host percentiles). |
| **Signal** | One named, normalized piece of evidence derived from a tree vector and a baseline. |
| **Override** | A signal class that sets a floor on the risk level rather than contributing to a weighted average. |
| **Verdict** | The scored output for one process or tree: score, level, contributing signals. |
| **Uncalibrated mode** | State before a baseline exists: overrides and absolute fallbacks only. |

---

## 2. The Event Contract (Frozen)

`internal/event` is the contract between collectors and everything above them. It is
**frozen**: adding a `Kind` is a compatible change; changing an existing field's meaning
is not.

```go
package event

type Kind uint8

const (
    KindUnknown Kind = iota
    KindProcessStart
    KindProcessExit
    KindFileCreate
    KindFileWrite
    KindFileRename
    KindFileDelete
    KindPersistenceInstall
    KindDecoyTouch
)

func (k Kind) String() string
```

```go
type Event struct {
    Seq      uint64    // assigned by the bus, monotonic per process
    Time     time.Time // wall clock at observation
    Kind     Kind
    Source   string    // emitting source name, for attribution and debugging

    // Actor
    PID      int32
    PPID     int32
    ProcName string
    Exe      string
    UID      uint32

    // Path
    Path     string    // primary path
    NewPath  string    // rename destination; empty unless KindFileRename

    // Content evidence (populated by FileWatch on write/create)
    Bytes         int64
    Entropy       float64 // Shannon entropy of sampled bytes, bits/byte
    MagicMismatch bool    // file magic disagrees with extension

    // Typed payloads
    DecoyID string    // set when Kind == KindDecoyTouch
    Persist string    // "cron" | "systemd" | "runkey" | "task" | "service"
}
```

### Field semantics

| Field | Semantics | Required for |
|---|---|---|
| `Seq` | Monotonic, assigned by the bus at publish. Used for ordering and dedup. | All |
| `Time` | Observation time, not event time. OS notification APIs do not supply the latter. | All |
| `PID` | The actor. Zero means unattributed. | All |
| `PPID` | Parent PID at observation time. Enables tree aggregation. | Process/file |
| `Path` | Absolute path where possible. | File kinds |
| `NewPath` | Rename destination. A rename to a previously-unseen extension is a primary signal. | `KindFileRename` |
| `Bytes` | Bytes written, when the OS reports it. Zero is legitimate (unknown), not an error. | `KindFileWrite` |
| `Entropy` | Shannon entropy $H = -\sum_i p_i \log_2 p_i$ over the head+tail sample. | `KindFileWrite` |
| `MagicMismatch` | True when the file's magic bytes contradict its extension. | `KindFileWrite` |
| `DecoyID` | Identifier of the touched canary, for post-incident attribution. | `KindDecoyTouch` |
| `Persist` | Which persistence mechanism was installed. | `KindPersistenceInstall` |

### Entropy sampling contract

`FileWatch` computes `Entropy` over a **head sample and a tail sample** (default 4 KiB
each), never the whole file. This is a specification, not an optimization:

- whole-file reads are unbounded I/O under an encryption storm and cause the sensor to
  fall behind the attack;
- head+tail also catches partial/intermittent encryption, which deliberately leaves the
  file's middle region in plaintext to depress whole-file entropy.

`Entropy` is `0` when the sample is empty or the file is unreadable. Consumers must
treat `0` as *unknown*, not as *low entropy*.

---

## 3. The Source Interface

```go
package sensor

type Source interface {
    Name() string
    Start(ctx context.Context, out chan<- event.Event) error
    Close() error
}
```

### Contract

1. `Start` returns once the source is observing, not when it finishes. It must not block
   for the lifetime of the process.
2. The source writes to `out` until `ctx` is cancelled or `Close` is called.
3. `Start` and `Close` are idempotent with respect to a second `Close` — closing twice
   must not panic.
4. A source that cannot observe on this host must return an error from `Start` and be
   skipped; it must not silently produce nothing. Silent no-op sensors are the failure
   mode that makes a detector look healthy while blind.
5. Sources must not block indefinitely on `out`. If the bus is saturated, apply the
   source's own bounded policy and count the loss.

---

## 4. The Event Bus

```go
package bus

type DropPolicy uint8

const (
    DropOldest DropPolicy = iota // default
    DropNewest
    Block                        // never used on the hot path
)

type Stats struct {
    Published uint64
    Dropped   uint64
    Subscribers map[string]uint64 // per-subscriber delivery counts
}

func New(capacity int, policy DropPolicy) *Bus
func (b *Bus) Publish(ev event.Event)
func (b *Bus) Subscribe(name string, capacity int) <-chan event.Event
func (b *Bus) Stats() Stats
func (b *Bus) Close()
```

### Semantics

- `Publish` **never blocks** under `DropOldest`/`DropNewest`. Under `Block` it blocks,
  which is provided for tests only.
- Every drop increments `Stats().Dropped`. Drops are never silent.
- `Subscribe` returns a receive-only channel. A slow subscriber does not stall other
  subscribers — each has its own buffer.
- The rule layer subscribes with a larger capacity than the fingerprint layer, because a
  dropped override is more costly than a dropped window sample.
- `Publish` assigns `Seq`.

**Rationale for bounded queues:** an encryption storm is an overload condition. An
unbounded queue converts overload into unbounded memory growth, which converts a
detection problem into an availability problem.

---

## 5. Signal Reference

Each signal is computed from a `TreeVector` and a `Baseline`, normalized to `0..1`, and
carries a human-readable `Detail` string. Every alert must be reconstructible from its
signals.

```go
package score

type Class uint8

const (
    ClassPrimary   Class = iota // contributes to weighted sum
    ClassSecondary              // contributes with lower weight
    ClassOverride               // sets a floor, never averaged
)

type Signal struct {
    Name   string
    Class  Class
    Value  float64 // normalized 0..1
    Detail string  // e.g. "entropy +4.1σ on 37 .docx files"
}
```

| # | Name | Class | Baseline input | Status |
|---|---|---|---|---|
| 1 | `entropy_deviation` | Primary | `EntropyByExt[ext]` | Implemented |
| 2 | `magic_mismatch` | Primary | none | Implemented |
| 3 | `write_burst` | Primary | `WriteRateByProc[name]`, falling back to `HostEventRate` | Implemented |
| 4 | `rename_burst` | Primary | `HostEventRate` | Implemented |
| 5 | `unknown_extension_activity` | Primary | `KnownExt` | Implemented |
| 6 | `delete_rate` | Secondary | `HostEventRate` | Implemented |
| 7 | `dir_fanout` | Secondary | `DirFanout` | Implemented |
| 8 | `cum_bytes_rewritten` | Secondary | none | Implemented |
| 9 | `write_rate_absolute` | Primary | none — uncalibrated fallback only | Implemented |
| 10 | `persistence_install` | Override | none | Implemented (rule `R-PERSIST-INSTALL`) |
| 11 | `static_reputation` | Secondary | none | **Phase 7** — not yet emitted |
| 12 | `decoy_touch` | Override | none | Implemented (rule `R-DECOY-TOUCH`) |
| 13 | `bus_drops` | Secondary | none | Implemented |

### Normalization rules

- Deviation signals clamp to `[0,1]` at a documented ceiling ($z \ge 6$ for entropy,
  $8\times$ baseline for rates). Unbounded normalization lets one runaway signal
  dominate fusion.
- **Signals below a noise floor of 0.05 are dropped.** Without this, a process that
  wrote a few kilobytes produces a verdict and every writing process looks like a
  detection.
- Signals with no baseline input are **unavailable** in uncalibrated mode and are
  omitted from the signal list rather than reported as `0`. Reporting `0` would
  misrepresent *unknown* as *benign*.
- `write_rate_absolute` exists precisely because omitting deviation signals leaves
  uncalibrated mode thin. It is a fixed bulk-modification threshold
  (`scoring.absolute_write_rate`, default 20 writes/s) and is superseded by
  `write_burst` the moment a baseline exists.
- `Detail` strings must state the measured value and the comparison, never just a score.

### Why `unknown_extension_activity` rather than a rename signal

`fsnotify` reports a rename's *source* path, not its destination — the destination
arrives as a separate create event. A signal keyed on rename destinations would therefore
never fire. Counting writes and creates per extension captures the same evidence (a mass
rename to `.locked` shows up as a burst of creates on an extension the host has never
seen) using data that is actually available.

---

## 6. Fingerprint Engine Reference

```go
package fingerprint

// TreeVector is the summed fingerprint of a process and all its descendants.
type TreeVector struct {
    Root     int32
    ProcName string
    PIDs     []int32

    // Decaying track: bounded by the ring and aged out by decay_half_life.
    Writes        int
    Creates       int
    Renames       int
    Deletes       int
    Bytes         int64
    Dirs          map[string]struct{}
    Entropy       []EntropySample
    MagicTotal    int
    MagicMismatch int

    // Non-decaying track.
    CumFilesRewritten int64
    CumBytesRewritten int64
    ExtActivity       map[string]int64

    WindowDuration time.Duration
}

type EntropySample struct{ Ext string; H float64 }

type Engine struct { /* single-writer; see docs/architecture.md §12 */ }

func NewEngine(cfg config.Config) *Engine
func (e *Engine) Apply(ev event.Event)
func (e *Engine) Window(pid int32) (Window, bool)
func (e *Engine) Snapshot() map[int32]Window
func (e *Engine) Roots() []int32
func (e *Engine) Aggregate(root int32) TreeVector
func (e *Engine) Reap(pid int32)
func (e *Engine) Live() int
```

### Semantics

- `Apply` is called from exactly one goroutine. No internal locking.
- The decaying track ages out entries older than `window.decay_half_life`. Samples live in
  a fixed-capacity ring (`ringCapacity`, 4096), so memory is bounded by live process count
  rather than uptime.
- The cumulative track **never decays**. This is the mechanism that answers Gap 4.
- `ExtActivity` counts writes and creates per extension, cumulatively. It is what
  `unknown_extension_activity` reads, and it needs no file content — so it survives the
  race where a file is renamed before the sensor can read it.
- `Aggregate(root)` sums the fingerprint of `root` and every descendant, and is what
  scoring consumes.
- `Reap(pid)` releases state on `KindProcessExit`, re-parenting orphans to root rather
  than dropping them from the tree.
- Events with `PID == 0` are **not discarded**: they go into a host-level fingerprint
  (`HostName`, PID 0) so their file-derived evidence still reaches scoring. Detection must
  not depend on attribution succeeding — see `sprints.md` §13.

### Not implemented: event n-grams

An event-type n-gram would be a useful discriminator, and `window.ngram_length` exists in
configuration. **No n-gram feature is computed and no n-gram signal is emitted.** The
config key is currently dead.

This is tracked as sprint item 2.1: either the feature is implemented and appears in
verdicts, or the claim and the config key are removed. Until then, treat the n-gram as
absent.

---

## 7. Calibration Reference

```go
package calibrate

type Dist struct {
    Mean   float64
    StdDev float64
    N      int
}

type Baseline struct {
    Version         int
    Host            string
    CapturedAt      time.Time
    WarmupDuration  time.Duration
    EntropyByExt    map[string]Dist
    WriteRateByProc map[string]float64
    HostEventRate   Dist
    DirFanout       Dist
}

func Capture(ctx context.Context, cfg config.Config, bus *bus.Bus) (*Baseline, error)
func Load(path string) (*Baseline, error)
func (b *Baseline) Save(path string) error
func (b *Baseline) Ready() bool
```

### Semantics

- `Capture` observes the host for `calibration.warmup` and records distributions. It
  runs before scoring is enabled.
- `Ready()` reports whether the baseline has enough samples to be used
  (`N >= calibration.min_samples`). Before that, GRIMA runs in uncalibrated mode.
- `StdDev` of `0` is replaced with a floor (`calibration.entropy_sigma_floor`, default
  `0.05`) to avoid division by zero on extensions that are always identical.
- Persisted as JSON at `calibration.baseline_path`. Version field allows schema
  migration; an unreadable or version-mismatched baseline is ignored, not fatal.
- Recalibration merges confirmed-benign observations so operator-confirmed workloads
  stop alerting. This is the feedback edge in the architecture diagram.

---

## 8. Scoring & Fusion Reference

```go
package score

type Level uint8

const (
    LevelInfo Level = iota
    LevelLow
    LevelMedium
    LevelHigh
    LevelCritical
)

type Verdict struct {
    PID         int32
    ProcName    string
    Score       float64 // 0..100
    Level       Level
    Signals     []Signal
    Override    string // rule ID that set the floor; empty if none
    Calibrated  bool
    EvaluatedAt time.Time
}

func (s *Scorer) Evaluate(tv fingerprint.TreeVector, b *calibrate.Baseline) Verdict
```

### Fusion algorithm

1. **Weighted sum.** $\text{raw} = \sum_i w_i \cdot v_i$ over Primary and Secondary
   signals, where $w_i$ comes from configuration. Weights are normalized so the raw sum
   is in `[0,1]`, then scaled to `0..100`.
2. **Override floor.** For each Override signal present, `level = max(level, rule_severity)`
   and `Override` is set to the rule ID. Overrides are **never** averaged; they set a
   minimum.
3. **Decay.** The decaying track's contribution is reduced by
   `exp(-Δt / decay_half_life)` when no further suspicious activity follows. The
   cumulative track is exempt.
4. **Level mapping.** Score bands map to levels via configuration
   (`scoring.level_bands`), and the override floor may raise the final level above the
   band implied by score alone.

### Invariants

- A verdict with a non-empty `Override` has `Level >= rule_severity`.
- A verdict in uncalibrated mode has `Calibrated == false` and contains no
  deviation-based signals.
- `Signals` is never empty for `Level >= LevelMedium`. A medium-or-higher alert without
  stated evidence is a bug, not a detection.

---

## 9. Rule Reference

```go
package rules

type Hit struct {
    RuleID   string
    Severity score.Level
    Detail   string
}

type Rule struct {
    ID          string
    Description string
    Severity    score.Level
    Match       func(event.Event) bool
}

type Engine struct { /* ... */ }

func NewEngine(cfg config.Config) *Engine
func (e *Engine) Evaluate(ev event.Event) (Hit, bool)
```

### Semantics

- Rules are **pure predicates over a single event**. No cross-event state, no window
  dependency. This keeps them instantaneous and trivially testable.
- Rules subscribe to the bus directly, so they are evaluated on arrival rather than at
  the next scoring tick.
- A rule hit produces an Override-class signal. It sets a floor; it does not add.
- Rules are data-driven where possible (`configs/grima.example.toml` lists process names
  and command patterns) so operators can extend them without a rebuild.

### Initial rule set

| Rule ID | Severity | Trigger |
|---|---|---|
| `R-SHADOW-DELETE` | Critical | `vssadmin delete shadows`, `wmic shadowcopy delete`, `wbadmin delete catalog`, `bcdedit /set recoveryenabled no` |
| `R-EVENTLOG-CLEAR` | Critical | `wevtutil cl` |
| `R-USN-DELETE` | High | `fsutil usn deletejournal` |
| `R-BACKUP-KILL` | High | Termination of configured backup/database process names |
| `R-DECOY-TOUCH` | Critical | `KindDecoyTouch` |
| `R-PERSIST-INSTALL` | Medium | `KindPersistenceInstall` |
| `R-MASS-RENAME` | High | Rename burst to a previously-unseen extension |

Detection logic for the first four is derived from published rule corpora; attribution
is recorded in `THIRD_PARTY_NOTICES.md`.

---

## 10. Response Reference

```go
package response

type Action uint8

const (
    Observe Action = iota
    Alert
    Suspend
)

type Handler struct { /* ... */ }

func NewHandler(cfg config.Config) *Handler
func (h *Handler) Apply(v score.Verdict) error
```

| Action | Effect | Default |
|---|---|---|
| `Observe` | Record the verdict; no side effect | **Yes** |
| `Alert` | Emit to the configured sink (log, file, webhook) | On `>= alert_min_level` |
| `Suspend` | `SIGSTOP` (POSIX) / `NtSuspendProcess` (Windows) | Opt-in only |

**Suspension is opt-in and gated.** Terminating or suspending a process on a heuristic
score is a denial-of-service primitive; a false positive against a database process is
worse than a missed detection that alerts. `response.suspend_min_level` must be
`Critical` and `response.enable_suspend` must be explicitly set.

---

## 11. Configuration Reference

TOML, loaded from `--config`. An annotated example lives at
`configs/grima.example.toml`. Defaults are chosen so the binary runs with no config file
at all, in observe-only mode.

```toml
[general]
monitor_paths  = ["/home", "/srv"]     # directories to watch
log_level      = "info"

[bus]
capacity       = 8192
drop_policy    = "drop_oldest"

[filewatch]
entropy_sample_bytes = 4096            # per head/tail sample
rescan_on_overflow   = true

[decoy]
enabled        = true
names          = ["_grima_canary.doc", "quarterly_report.xlsx"]
count_per_dir  = 2

[window]
decay_half_life = "30s"
ngram_length    = 32

[calibration]
warmup         = "10m"
min_samples    = 200
baseline_path  = "grima-baseline.json"
entropy_sigma_floor = 0.05

[scoring]
level_bands        = { low = 20, medium = 45, high = 70, critical = 88 }
absolute_write_rate = 20               # used only while uncalibrated
[[scoring.weights]]
name = "entropy_deviation"
weight = 1.0
# ... one entry per signal

[response]
enable_suspend    = false
suspend_min_level = "critical"
alert_min_level   = "medium"

[web]
enabled = true
listen  = "127.0.0.1:8787"
```

---

## 12. Observability & Health

GRIMA reports on itself, because a detector that is silently blind is worse than no
detector.

| Metric | Meaning | Surfaced where |
|---|---|---|
| `bus.published` | Events published | `/healthz`, dashboard |
| `bus.dropped` | Events dropped (overload evidence) | `/healthz`, dashboard, signal #13 |
| `sensor.<name>.events` | Per-source event count | `/healthz` |
| `sensor.<name>.errors` | Per-source error count | `/healthz` |
| `watch.failures` | inotify watch exhaustion / RDCW overflow | `/healthz` |
| `calibration.ready` | Whether deviation signals are active | dashboard banner |
| `calibration.age` | Time since baseline capture | dashboard |

`/healthz` returns JSON. The dashboard shows a persistent banner while in uncalibrated
mode, so an operator never mistakes "no alerts" for "calibrated and quiet."

---

## 13. Error Handling

- Sensor failures are **non-fatal and loud**. One failing source degrades coverage; it
  does not stop the pipeline. The failure is counted and surfaced.
- A malformed or version-mismatched baseline is ignored with a warning; GRIMA falls back
  to uncalibrated mode rather than refusing to start.
- Rule predicate panics are recovered and counted. A broken rule must not take down the
  detector.
- All errors carry the source name and the event sequence number when an event is
  involved.

---

## 14. Testing Contract

- The event schema is frozen; tests assert on `Event` values, not on sensor internals.
- The fingerprint engine is single-writer, so tests drive it directly with synthetic
  events and assert on window state — no timing, no sleeps, deterministic.
- Rules are pure predicates; each rule gets a table-driven test over events that should
  and should not match.
- Scoring is tested against synthetic baselines with known distributions, asserting that
  $z$-score normalization and clamping behave at the boundaries ($z=0$, $z=6$, $\sigma=0$
  floor).
- A bus test asserts that drops are counted rather than lost silently.

Tests must assert **observable contract**: window counts, verdict levels, signal
presence, drop counters. They must not assert implementation details such as internal
map identities or log wording.

---

## 15. Non-Goals

- Kernel drivers, minifilters, or eBPF programs. User space only.
- Machine learning of any kind. No training, no pretrained artifacts, no inference
  runtime.
- Pre-execution blocking. GRIMA observes; it does not gate execution.
- Decryption, key recovery, or file restoration.
- Network traffic inspection or C2 detection.
- Multi-host correlation or a central management server. Single host, single binary.
