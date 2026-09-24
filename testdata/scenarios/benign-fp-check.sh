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
#   GRIMA_FP_DURATION   detector run length (default 90s)
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

PYTHON="${GRIMA_FP_PYTHON:-$(command -v python3 || command -v python)}" \
  || { echo "error: python is required for the verdict dump" >&2; exit 2; }

PORT="${GRIMA_FP_PORT:-8794}"
DURATION="${GRIMA_FP_DURATION:-90s}"
WORK="${GRIMA_FP_WORK:-${TMPDIR:-${TEMP:-${TMP:-/tmp}}}/grima-fp}/$NAME"
DATA="$WORK/data"
mkdir -p "$WORK"
rm -rf "$DATA"
mkdir -p "$DATA"

echo "false-positive check: $NAME"
echo "work directory: $DATA"
echo "detector: $BINARY on port $PORT"

cat > "$WORK/grima.toml" <<EOF
[general]
monitor_paths = ["$(to_host_path "$DATA")"]
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

# --- calibration: the workload runs while the baseline is captured -----------

"$BINARY" --config "$(to_host_path "$WORK/grima.toml")" --calibrate > "$WORK/calibrate.log" 2>&1 &
CAL_PID=$!

calibration_passes=0
while kill -0 "$CAL_PID" 2>/dev/null && [ "$calibration_passes" -lt 12 ]; do
  run_scenario "$WORK/warmup.log" "$@" || true
  calibration_passes=$((calibration_passes + 1))
done
wait "$CAL_PID"

if ! grep -q 'baseline written' "$WORK/calibrate.log"; then
  cat "$WORK/calibrate.log" >&2
  echo "error: calibration produced no baseline" >&2
  exit 2
fi
echo "baseline captured after $calibration_passes workload pass(es)"

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
tail -1 "$WORK/workload.log"
echo "measured pass: ${elapsed}s"

sleep 8
curl -fsS --max-time 5 "http://127.0.0.1:$PORT/api/verdicts" > "$WORK/verdicts.json" 2>/dev/null
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

echo
if [ "$alerts" = "0" ]; then
  echo "PASS: $NAME stayed below response.alert_min_level"
  exit 0
fi
echo "FALSE POSITIVE: $NAME produced $alerts alert line(s)"
exit 1
