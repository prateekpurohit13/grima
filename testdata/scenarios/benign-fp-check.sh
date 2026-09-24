#!/usr/bin/env bash
# Run one benign scenario against a calibrated detector and report whether the
# detector alerted on it.
#
# This is the false-positive half of the corpus: the workload is legitimate bulk
# I/O, so any verdict at or above response.alert_min_level is a false positive
# and the script prints the verdict with its signals instead of hiding it.
#
# The workload is run once during the detector's calibration and again for the
# measured pass, so the host being watched has the shape of a machine that has
# used that tool before: the extensions it writes and the entropy ranges it
# produces are baseline data, not novelty.
#
# Usage: benign-fp-check.sh <scenario-script> [extra scenario args...]
#   the scenario must accept its work directory as its first argument
#
# Environment overrides:
#   GRIMA_BINARY        detector binary (default: <repo>/grima[.exe])
#   GRIMA_FP_PORT       dashboard port (default 8794)
#   GRIMA_FP_WORK       work root (default $TMPDIR/grima-fp)
#   GRIMA_FP_DURATION   minimum detector run length (default 90s; raised when
#                       the workload takes longer, so the detector outlives it)
#   GRIMA_FP_PYTHON     interpreter for the end-of-run verdict dump
#
# Exits 0 when the workload stayed silent, 1 when it produced an alert.
set -uo pipefail

ROOT_SH="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

to_host_path() {
  local path="$1" converted=""
  case "$path" in
    [A-Za-z]:[\\/]*) printf '%s' "$path" | tr '\\' '/' ; return ;;
  esac
  if command -v cygpath >/dev/null 2>&1; then
    converted="$(cygpath -w "$path" 2>/dev/null)"
  elif command -v wslpath >/dev/null 2>&1; then
    converted="$(wslpath -w "$path" 2>/dev/null)"
  fi
  if [ -n "$converted" ]; then
    printf '%s' "$converted" | tr '\\' '/'
  else
    printf '%s' "$path"
  fi
}

if [ "$#" -lt 1 ]; then
  echo "usage: benign-fp-check.sh <scenario-script> [extra args...]" >&2
  exit 2
fi

SCRIPT="$1"; shift
[ -f "$SCRIPT" ] || { echo "error: no such scenario: $SCRIPT" >&2; exit 2; }
NAME="$(basename "$SCRIPT" .sh)"

BINARY="${GRIMA_BINARY:-$ROOT_SH/grima}"
[ -x "$BINARY" ] || BINARY="$ROOT_SH/grima.exe"
[ -x "$BINARY" ] || { echo "error: build grima first (make build), or set GRIMA_BINARY" >&2; exit 2; }

# A `python3` on PATH may be a Windows Store stub that prints an install message
# and does nothing, so each candidate has to prove it runs.
pick_python() {
  local candidate
  for candidate in "$@"; do
    [ -n "$candidate" ] || continue
    if "$candidate" -c 'import sys' >/dev/null 2>&1; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  return 1
}

PYTHON="$(pick_python "${GRIMA_FP_PYTHON:-}" python3 python)" \
  || { echo "error: python is required for the verdict dump" >&2; exit 2; }

PORT="${GRIMA_FP_PORT:-8794}"
DURATION="${GRIMA_FP_DURATION:-90s}"
WORK="${GRIMA_FP_WORK:-${TMPDIR:-${TEMP:-${TMP:-/tmp}}}/grima-fp}/$NAME"
# The workload works inside a monitored tree rather than being the tree: most of
# these scripts reset their own work directory at the start, and deleting the
# directory the sensor is watching removes the watch, after which nothing is
# seen and the baseline comes out empty.
MONITOR="$WORK/monitored"
DATA="$MONITOR/data"
mkdir -p "$WORK"
rm -rf "$MONITOR"
mkdir -p "$DATA"

echo "false-positive check: $NAME"
echo "work directory: $DATA"
echo "detector: $BINARY on port $PORT"

cat > "$WORK/grima.toml" <<EOF
[general]
monitor_paths = ["$(to_host_path "$MONITOR")"]
log_level = "info"

[web]
enabled = true
listen = "127.0.0.1:$PORT"

[decoy]
enabled = false

[calibration]
warmup = "15s"
min_samples = 20
baseline_path = "$(to_host_path "$WORK/baseline.json")"

[procwatch]
sample_interval = "500ms"
EOF

run_scenario() { # $1 log file, rest: extra scenario args
  local log="$1"; shift
  "${BASH:-bash}" "$SCRIPT" "$DATA" "$@" > "$log" 2>&1
}

# GNU tar reads `C:/...` as a remote `host:path` and fails, so a workload that
# archives by absolute path silently produces no archive under Git-Bash or WSL
# even though the detector run itself is fine. `--force-local` is the documented
# GNU answer; bsdtar has no such rule and no such flag, so it is only added when
# the tar on PATH is GNU. (benign-archive.sh's archiver choice is not
# drive-letter safe; this keeps the corpus runnable, it does not fix that.)
if tar --version 2>/dev/null | grep -q 'GNU tar'; then
  export TAR_OPTIONS="--force-local ${TAR_OPTIONS:-}"
fi

# --- calibration: the workload runs while the baseline is captured -----------

"$BINARY" --config "$(to_host_path "$WORK/grima.toml")" --calibrate > "$WORK/calibrate.log" 2>&1 &
CAL_PID=$!

calibration_passes=0
slowest_pass=0
cal_deadline=$(( $(date +%s) + 18 ))
while [ "$(date +%s)" -lt "$cal_deadline" ]; do
  pass_start="$(date +%s)"
  run_scenario "$WORK/warmup.log" "$@" || true
  pass_elapsed=$(( $(date +%s) - pass_start ))
  [ "$pass_elapsed" -gt "$slowest_pass" ] && slowest_pass="$pass_elapsed"
  calibration_passes=$((calibration_passes + 1))
done
wait "$CAL_PID"

if ! grep -q 'baseline written' "$WORK/calibrate.log"; then
  cat "$WORK/calibrate.log" >&2
  echo "error: calibration produced no baseline" >&2
  exit 2
fi
echo "baseline captured after $calibration_passes workload pass(es)"
grep 'baseline written' "$WORK/calibrate.log" | tail -1 | sed 's/^/  /'

# The detector has to outlive the workload. A detector that reaches --duration
# mid-workload closes its dashboard, so the run ends with no verdicts to read
# and a workload that was only half watched looks like a silent one. The
# warm-up passes just timed the workload, so size the measured run from the
# slowest of them. Only a plain seconds value can be resized; anything else
# (e.g. "1m30s") is left exactly as the operator set it.
DURATION_SECS="${DURATION%s}"
case "$DURATION_SECS" in
  ''|*[!0-9]*) DURATION_SECS=0 ;;
esac
if [ "$DURATION_SECS" -gt 0 ]; then
  needed=$(( slowest_pass + 30 ))
  if [ "$needed" -gt "$DURATION_SECS" ]; then
    echo "detector run extended to ${needed}s: the workload took ${slowest_pass}s in warm-up"
    DURATION="${needed}s"
  fi
fi

# --- measured run -----------------------------------------------------------

"$BINARY" --config "$(to_host_path "$WORK/grima.toml")" --duration "$DURATION" > "$WORK/grima.log" 2>&1 &
DET_PID=$!
trap 'kill "$DET_PID" 2>/dev/null' EXIT
sleep 5

if grep -q 'no usable baseline' "$WORK/grima.log"; then
  echo "error: detector ran uncalibrated despite a captured baseline" >&2
  exit 2
fi

start="$(date +%s)"
run_scenario "$WORK/workload.log" "$@"
elapsed=$(( $(date +%s) - start ))

# A workload that aborted writes little or nothing, and "little" also produces
# no alert — so a silent verdict from a failed workload is not a measurement.
# Every benign workload ends with a SUMMARY line; without one, say so.
if ! grep -q '^SUMMARY ' "$WORK/workload.log"; then
  echo "error: the workload did not complete (no SUMMARY line), so a silent" >&2
  echo "       verdict would be meaningless:" >&2
  tail -5 "$WORK/workload.log" >&2
  kill "$DET_PID" 2>/dev/null
  wait "$DET_PID" 2>/dev/null
  trap - EXIT
  exit 2
fi
tail -1 "$WORK/workload.log"
echo "measured pass: ${elapsed}s"

# The sizing above is an estimate from the warm-up passes; if the measured pass
# still outran it, the detector is gone and so are its verdicts. Say so rather
# than reporting a workload nobody was watching.
if ! kill -0 "$DET_PID" 2>/dev/null; then
  echo "error: the detector exited before the workload finished (--duration $DURATION)," >&2
  echo "       so its verdicts were never collected; raise GRIMA_FP_DURATION" >&2
  tail -3 "$WORK/grima.log" >&2
  trap - EXIT
  exit 2
fi

sleep 8
curl -fsS --max-time 5 "http://127.0.0.1:$PORT/api/verdicts" > "$WORK/verdicts.json" 2>/dev/null

# A silent verdict only means something if the detector saw the workload, so the
# sensor counters are recorded next to the verdict they justify.
if curl -fsS --max-time 5 "http://127.0.0.1:$PORT/healthz" > "$WORK/healthz.json" 2>/dev/null; then
  "$PYTHON" - "$WORK/healthz.json" <<'PY'
import json, sys
h = json.load(open(sys.argv[1]))
fw = (h.get("sensors") or {}).get("filewatch") or {}
print("observed: calibration_ready=%s filewatch_events=%s filewatch_reporting=%s"
      % (h.get("calibration_ready"), fw.get("Events"), fw.get("Reporting")))
PY
else
  echo "observed: health endpoint unreachable from this shell"
fi

kill "$DET_PID" 2>/dev/null
wait "$DET_PID" 2>/dev/null
trap - EXIT

# --- verdict ----------------------------------------------------------------

echo
echo "--- verdicts at the end of the run ---"
if [ -s "$WORK/verdicts.json" ]; then
  "$PYTHON" - "$WORK/verdicts.json" <<'PY'
import json, sys
levels = ["info", "low", "medium", "high", "critical"]
for v in json.load(open(sys.argv[1])):
    signals = "; ".join("%s=%.3f" % (s["Name"], s["Value"]) for s in (v.get("Signals") or []))
    print("score=%.1f level=%s pid=%s proc=%s signals=%s"
          % (v["Score"], levels[v["Level"]], v["PID"], v["ProcName"], signals or "-"))
PY
else
  echo "(no verdict published)"
fi

alerts="$(grep -c 'ransomware risk detected' "$WORK/grima.log")"
echo
echo "--- detector alerts during the measured pass ---"
if [ "$alerts" = "0" ]; then
  echo "none"
else
  grep 'ransomware risk detected' "$WORK/grima.log" | sed -n '1p;$p'
fi
echo "logs: $WORK"

# One machine-readable line, so a spread report or any other consumer reads a
# parsed result instead of re-parsing the human-readable verdict dump above.
"$PYTHON" - "$WORK/verdicts.json" "$NAME" "$alerts" <<'PY' || \
  echo "RESULT scenario=$NAME max_score=unavailable max_level=unavailable max_signals=- alerts=$alerts"
import json, os, sys

path, name, alerts = sys.argv[1], sys.argv[2], sys.argv[3]
levels = ["info", "low", "medium", "high", "critical"]
verdicts = []
if os.path.exists(path) and os.path.getsize(path):
    try:
        verdicts = json.load(open(path))
    except ValueError:
        verdicts = []
if not verdicts:
    print("RESULT scenario=%s max_score=none max_level=none max_signals=- alerts=%s" % (name, alerts))
else:
    peak = max(verdicts, key=lambda v: v["Score"])
    names = ",".join(s["Name"] for s in (peak.get("Signals") or [])) or "-"
    print("RESULT scenario=%s max_score=%.1f max_level=%s max_signals=%s alerts=%s"
          % (name, peak["Score"], levels[peak["Level"]], names, alerts))
PY

echo
if [ "$alerts" = "0" ]; then
  echo "PASS: $NAME stayed below response.alert_min_level"
  exit 0
fi
echo "FALSE POSITIVE: $NAME produced $alerts alert line(s)"
exit 1
