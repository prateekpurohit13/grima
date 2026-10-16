#!/usr/bin/env bash
# Scripted walkthrough of every GRIMA scenario, for a live demonstration.
#
# Every phase states the claim it demonstrates, runs the detector against a real
# workload, and asserts the outcome from the detector's own verdicts and log.
# Nothing here is mocked: the workloads are the same fixtures the measurement
# harnesses use, and the detector is the shipped binary with a phase-local
# config derived from configs/grima.example.toml.
#
# Usage: demo.sh [options]
#   --quick            five core phases (~4 min instead of ~10)
#   --interactive      pause between phases so the dashboard can be shown
#   --keep             keep the work root (verdict logs, baselines, configs)
#   --phases a,b,c     run only these phases, in the order given
#   --work DIR         work root (default: a fresh mktemp directory)
#   --list             print the phase names and what each one claims
#
# Environment:
#   GRIMA_BINARY       detector binary (default: <repo>/grima[.exe])
#   GRIMA_DEMO_PORT    first dashboard port (default 8871; one per phase)
#   GRIMA_DEMO_WORK    work root (default mktemp -d)
#   GRIMA_DEMO_PYTHON  interpreter for the fixtures
#   GRIMA_DEMO_KEEP_WORK  as --keep
#
# Nothing outside the work root is touched: monitor_paths, the baseline file and
# the decoy manifest all live inside it, and decoys are off except in the decoy
# phase.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BINARY="${GRIMA_BINARY:-}"
if [ -z "$BINARY" ]; then
  if [ -x "$ROOT/grima" ]; then BINARY="$ROOT/grima"
  elif [ -x "$ROOT/grima.exe" ]; then BINARY="$ROOT/grima.exe"
  fi
fi

ALL_PHASES="build burst benign create-storm intermittent tail drip decoy rules rule-tests split uncalibrated observability"
QUICK_PHASES="build burst benign decoy uncalibrated"
PHASES="$ALL_PHASES"
PORT_BASE="${GRIMA_DEMO_PORT:-8871}"
WARMUP_SECS=15
KEEP_WORK="${GRIMA_DEMO_KEEP_WORK:-}"
INTERACTIVE=""
WORK="${GRIMA_DEMO_WORK:-}"

while [ $# -gt 0 ]; do
  case "$1" in
    --quick) PHASES="$QUICK_PHASES" ;;
    --interactive) INTERACTIVE=1 ;;
    --keep) KEEP_WORK=1 ;;
    --phases) shift; PHASES="${1//,/ }" ;;
    --work) shift; WORK="$1" ;;
    --list)
      cat <<'LIST'
build          the artifact is one static binary; no interpreter, no loader, no cgo
burst          multi-signal fusion detects mass encryption, at critical, in a few hundred KiB
benign         legitimate bulk temp-write-and-rename stays silent with a calibrated baseline
create-storm   a create-only storm is scored (create_burst), not invisible
intermittent   partial encryption that leaves the file's middle plaintext is still caught
tail           the tail sample is read; a tail-only overwrite would be missed head-only
drip           drip encryption that never fills the window is caught by the cumulative track
decoy          a canary touch is a critical override, not a score
rules          a hard rule fires on a raw event and raises the floor instantly
rule-tests     the rule predicates pass their own happy/sad-path tests
split          a cooperative multi-process split is detected across processes
uncalibrated   before a baseline exists the detector says so, and unknown is not benign
observability  every bounded queue counts what it lost
LIST
      exit 0
      ;;
    -h|--help) sed -n '2,30p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $1 (try --list)" >&2; exit 2 ;;
  esac
  shift
done

[ -x "$BINARY" ] || { echo "error: no detector at ${BINARY:-<repo>/grima}; run 'make build' or set GRIMA_BINARY" >&2; exit 2; }
[ -n "$WORK" ] || WORK="$(mktemp -d -t grima-demo-XXXXXX)"
mkdir -p "$WORK"

# A python on PATH must prove it evaluates code: a resolver that only checks the
# command exists accepts a shim that then fails to open any script.
pick_python() {
  local cand
  for cand in ${GRIMA_DEMO_PYTHON:-} python3 python; do
    [ -n "$cand" ] || continue
    if command -v "$cand" >/dev/null 2>&1 &&
       [ "$("$cand" -c 'print(4**2)' 2>/dev/null)" = "16" ]; then
      echo "$cand"; return 0
    fi
  done
  return 1
}
PYTHON="$(pick_python)" || { echo "error: no working python3/python; the fixtures need one" >&2; exit 2; }
export GRIMA_DEMO_PY="$PYTHON"
command -v curl >/dev/null 2>&1 || { echo "error: curl is required to read the dashboard API" >&2; exit 2; }

PASSED=0; FAILED=0; SKIPPED=0
declare -a RESULTS=()
PHASE_N=0
DET_PID=""; WARM_PID=""; EXTRA_PID=""

cleanup() {
  for p in "$DET_PID" "$WARM_PID" "$EXTRA_PID"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done
  if [ -z "$KEEP_WORK" ]; then rm -rf "$WORK"; else printf '\nwork root kept: %s\n' "$WORK"; fi
}
trap cleanup EXIT

say()  { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '    %s\n' "$*"; }
record() { RESULTS+=("$1|$2|$3"); case "$1" in PASS) PASSED=$((PASSED+1));; SKIP) SKIPPED=$((SKIPPED+1));; *) FAILED=$((FAILED+1));; esac; }
ok()   { record PASS "$1" "$2"; printf '    \033[32mPASS\033[0m: %s\n' "$2"; }
bad()  { record FAIL "$1" "$2"; printf '    \033[31mFAIL\033[0m: %s\n' "$2"; }
skip() { record SKIP "$1" "$2"; printf '    \033[33mSKIP\033[0m: %s\n' "$2"; }
want_phase() { case " $PHASES " in *" $1 "*) return 0;; *) return 1;; esac; }

pause_for_demo() { # port
  [ -n "$INTERACTIVE" ] || return 0
  printf '\n    dashboard: http://127.0.0.1:%s/   (Ctrl-C stops the demo)\n' "$1"
  printf '    press Enter to continue...'
  read -r _ </dev/tty || true
}

phase_dir()  { local d="$WORK/$1"; mkdir -p "$d/data" "$d/log"; echo "$d"; }
# next_port sets PORT. It must not run in a command substitution: PHASE_N is
# mutated here, and a subshell would discard the increment, handing every phase
# the same port.
PORT=0
next_port() { PHASE_N=$((PHASE_N+1)); PORT=$((PORT_BASE + PHASE_N - 1)); }

# make_config derives a phase config from the annotated example, overriding only
# what a demo needs: the monitored tree, the output paths, the port, and the
# knobs a phase's meaning depends on (baseline presence, window length,
# attribution mode, decoy planting).
make_config() { # dir port decoys(0|1) decay half-life baseline(0|1) -> path
  local d="$1" port="$2" decoys="$3" decay="$4" baseline="$5"
  local cfg="$d/grima.toml" base="$d/baseline.json"
  [ "$baseline" = "1" ] || base="$d/no-baseline.json"
  awk -v monitor="$d/data" -v dir="$d" -v base="$base" -v port="$port" \
      -v decoys="$decoys" -v decay="$decay" '
    { if ($0 ~ /^\[/) sect = $0 }
    /^monitor_paths = /  { print "monitor_paths = [\"" monitor "\"]"; next }
    /^warmup = /         { print "warmup = \"15s\""; next }
    /^min_samples = /    { print "min_samples = 20"; next }
    /^baseline_path = /  { print "baseline_path = \"" base "\""; next }
    /^manifest_path = /  { print "manifest_path = \"" dir "/decoys.json\""; next }
    /^listen = /         { print "listen = \"127.0.0.1:" port "\""; next }
    /^decay_half_life = /{ print "decay_half_life = \"" decay "\""; next }
    /^mode = /           { print "mode = \"host\""; next }
    sect == "[decoy]" && /^enabled = / { print "enabled = " (decoys ? "true" : "false"); next }
    { print }
  ' "$ROOT/configs/grima.example.toml" > "$cfg"
  echo "$cfg"
}

# warm_up drives ordinary writes into the monitored tree. Calibration measures
# this host's normal, so an idle warm-up measures nothing: the baseline records
# zero samples and every deviation signal stays unavailable (measured: the same
# encryptor that alerts at critical is missed with a zero-sample baseline).
warm_up() { # dir seconds
  local dir="$1" secs="$2"
  local end=$((SECONDS+secs)) i=0
  while [ $SECONDS -lt $end ]; do
    i=$((i+1))
    printf 'quarterly report %s\nsecond line of the report\n' "$i" > "$dir/notes_$((i % 20)).txt"
    { printf 'PK\003\004'; printf 'office payload revision %s\n' "$i"; } > "$dir/ledger_$((i % 5)).docx"
    sleep 0.25
  done
}

# calibrate runs a warm-up for the whole warm-up window. An optional extra
# command is looped alongside it, so a phase whose workload uses an extension
# the generic warm-up never writes (.conf, .pdf, .dat) still has that extension
# in the baseline instead of arriving as first-seen novelty.
calibrate() { # dir cfg [extra-command]
  local d="$1" cfg="$2" extra="${3:-}"
  : > "$d/log/calibrate.log"
  "$BINARY" --config "$cfg" --calibrate >> "$d/log/calibrate.log" 2>&1 &
  local cal=$!
  warm_up "$d/data" $((WARMUP_SECS+4)) & WARM_PID=$!
  if [ -n "$extra" ]; then
    ( end=$((SECONDS+WARMUP_SECS+3)); while [ $SECONDS -lt $end ]; do eval "$extra"; sleep 0.2; done ) &
    EXTRA_PID=$!
  fi
  wait "$cal"
  kill "$WARM_PID" "$EXTRA_PID" 2>/dev/null; WARM_PID=""; EXTRA_PID=""
  local line
  line="$(grep -m1 'baseline written' "$d/log/calibrate.log" || true)"
  if [ -z "$line" ]; then
    note "calibration wrote no baseline:"; sed -n '1,4p' "$d/log/calibrate.log"; return 1
  fi
  note "$(printf '%s' "$line" | sed 's/^time=[^ ]* //')"
  case "$line" in *extensions=0*) note "WARNING: baseline has no samples — the warm-up was idle" ;; esac
  return 0
}

start_detector() { # cfg log duration port
  : > "$2"
  "$BINARY" --config "$1" --duration "$3" > "$2" 2>&1 &
  DET_PID=$!
  local i
  for i in $(seq 1 75); do
    curl -fsS "http://127.0.0.1:$4/healthz" >/dev/null 2>&1 && return 0
    kill -0 "$DET_PID" 2>/dev/null || return 1
    sleep 0.2
  done
  return 1
}
finish_phase() { kill "$DET_PID" 2>/dev/null; wait "$DET_PID" 2>/dev/null; DET_PID=""; }

verdicts() { # port -> "level score pid process signals", best first
  curl -fsS "http://127.0.0.1:$1/api/verdicts" 2>/dev/null | "$PYTHON" -c '
import json,sys
ranks={0:"info",1:"low",2:"medium",3:"high",4:"critical"}
try: vs=json.load(sys.stdin)
except Exception: vs=[]
for v in vs[:4]:
    print(ranks.get(v["Level"],"?"), "%.0f"%v["Score"], v["PID"], v["ProcName"],
          ",".join(s["Name"] for s in v["Signals"]) or "(no signals)")
'
}

peak() { # log -> "<rank>\n<worst alert line>"
  "$PYTHON" - "$1" <<'PY'
import re,sys
ranks={'info':0,'low':1,'medium':2,'high':3,'critical':4}
best=-1; line=''
for raw in open(sys.argv[1],errors='replace'):
    m=re.search(r'msg="ransomware risk detected".*?level=(\w+)',raw)
    if not m: continue
    r=ranks.get(m.group(1),-1)
    if r>best: best=r; line=raw.strip()
print(best)
print(line)
PY
}

assert_alert() { # phase log min-rank [required-signal-regex]
  local phase="$1" log="$2" min="$3" want="${4:-}" out got line
  out="$(peak "$log")"
  got="$(printf '%s\n' "$out" | sed -n 1p)"
  line="$(printf '%s\n' "$out" | sed -n 2p)"
  if [ "${got:--1}" -lt "$min" ]; then
    bad "$phase" "no alert at or above rank $min (worst was ${got:--1}); evidence: $log"
    return 1
  fi
  if [ -n "$want" ] && ! printf '%s' "$line" | grep -qE "$want"; then
    bad "$phase" "alerted, but without a matching signal ($want)"; note "$line"; return 1
  fi
  ok "$phase" "$(printf '%s' "$line" | sed 's/^time=[^ ]* //; s/ msg="ransomware risk detected"//')"
  return 0
}

assert_silent() { # phase log
  if grep -q 'ransomware risk detected' "$1"; then
    bad "$2" "a benign workload alerted"; grep -m2 'ransomware risk detected' "$2"
    return 1
  fi
  ok "$2" "benign workload stayed below the alert band (0 alerts)"
  return 0
}

# ---------------------------------------------------------------------------

run_build() {
  want_phase build || return 0
  say "setup. build — one static binary, no interpreter, no loader, no cgo"
  note "$("$BINARY" --version)"
  note "binary: $BINARY"
  note "flags:  --config --duration --calibrate --recalibrate --remove-decoys --version"
  if command -v file >/dev/null 2>&1; then note "$(file -b "$BINARY" | cut -c1-96)"; fi
  ok build "the artifact runs and reports its version"
}

run_burst() {
  want_phase burst || return 0
  local d port cfg i
  d="$(phase_dir burst)"; next_port; port="$PORT"; cfg="$(make_config "$d" "$port" 0 30s 1)"
  say "case 1. burst encryption — mass modification, detected at critical"
  note "claim: independent signals fuse into one critical verdict, and the verdict names its evidence"
  for i in $(seq 1 40); do printf 'report %s\nsecond line of the report\n' "$i" > "$d/data/report_$i.txt"; done
  for i in $(seq 1 15); do { printf 'PK\003\004'; printf 'office payload %s\n' "$i"; } > "$d/data/doc_$i.docx"; done
  calibrate "$d" "$cfg" || { bad burst "calibration failed"; return 1; }
  start_detector "$cfg" "$d/log/run.log" 30s "$port" || { bad burst "the detector never answered /healthz"; return 1; }
  pause_for_demo "$port"
  sleep 3
  note "workload: encryptor.py --rate burst on 55 files ($d/data)"
  "$PYTHON" "$ROOT/testdata/scenarios/encryptor.py" --path "$d/data" --rate burst --seed 7 2>&1 | tail -1
  sleep 10
  note "verdicts now held:"; verdicts "$port" | sed 's/^/      /'
  assert_alert burst "$d/log/run.log" 4 'entropy_deviation|magic_mismatch|unknown_extension_activity|create_burst|rename_burst|ngram_rename_chain'
  finish_phase
}

run_benign() {
  want_phase benign || return 0
  local d port cfg i j round
  d="$(phase_dir benign)"; next_port; port="$PORT"; cfg="$(make_config "$d" "$port" 0 30s 1)"
  say "case 2. benign bulk rewriting — the false-positive control"
  note "claim: with a baseline measured on this host, temp-write-then-rename traffic stays silent"
  for i in $(seq 1 8); do mkdir -p "$d/data/config-$i"; done
  for i in $(seq 1 8); do
    for j in $(seq 1 60); do printf 'service config %s %s\n' "$i" "$j" > "$d/data/config-$i/service_$j.conf"; done
  done
  # The baseline has to see this workload's own shape: the temp names it creates
  # and the rates it produces. A warm-up that writes only the final files leaves
  # .tmp as a first-seen extension and the create rate far below the workload's,
  # which is exactly the false positive this phase exists to rule out.
  cat > "$d/warm_atomic.sh" <<'SH'
#!/usr/bin/env bash
d="$1"
for i in 1 2 3 4 5 6 7 8; do
  for j in $(seq 1 60); do
    tmp="$d/config-$i/.service_$j.conf.tmp"
    [ $((j % 3)) -eq 0 ] && tmp="$d/config-$i/.service_$j.conf.swp"
    printf 'service config %s %s\n' "$i" "$j" > "$tmp"
    mv -f "$tmp" "$d/config-$i/service_$j.conf"
  done
done
SH
  calibrate "$d" "$cfg" "bash '$d/warm_atomic.sh' '$d/data'" || { bad benign "calibration failed"; return 1; }
  start_detector "$cfg" "$d/log/run.log" 50s "$port" || { bad benign "the detector never answered /healthz"; return 1; }
  pause_for_demo "$port"
  note "workload: 3 rounds of 480 write-temp-then-rename into 8 directories (the atomic-save shape)"
  cat > "$d/atomic_round.sh" <<'SH'
#!/usr/bin/env bash
d="$1"
for i in 1 2 3 4 5 6 7 8; do
  for j in $(seq 1 60); do
    tmp="$d/config-$i/.service_$j.conf.tmp"
    [ $((j % 3)) -eq 0 ] && tmp="$d/config-$i/.service_$j.conf.swp"
    printf 'service config %s %s rewritten\n' "$i" "$j" > "$tmp"
    mv -f "$tmp" "$d/config-$i/service_$j.conf"
  done
done
SH
  for round in 1 2 3; do
    bash "$d/atomic_round.sh" "$d/data"
    sleep 1
  done
  sleep 6
  note "verdicts now held:"; verdicts "$port" | sed 's/^/      /'
  assert_silent "$d/log/run.log" benign
  finish_phase
}

run_create_storm() {
  want_phase create-storm || return 0
  local d port cfg i
  d="$(phase_dir create-storm)"; next_port; port="$PORT"; cfg="$(make_config "$d" "$port" 0 30s 1)"
  say "case 3. create-only storm — creates are counted, so they are scored"
  note "claim: a process that only creates files is not invisible"
  note "       (measured bug this closes: 12,026 creates in one second produced no verdict)"
  calibrate "$d" "$cfg" || { bad create-storm "calibration failed"; return 1; }
  start_detector "$cfg" "$d/log/run.log" 30s "$port" || { bad create-storm "the detector never answered /healthz"; return 1; }
  pause_for_demo "$port"
  sleep 3
  note "workload: 4,000 new files created inside the monitored tree, none of them written"
  for i in $(seq 1 4000); do : > "$d/data/blob_$i.bin"; done
  sleep 8
  note "verdicts now held:"; verdicts "$port" | sed 's/^/      /'
  assert_alert create-storm "$d/log/run.log" 2 'create_burst|write_rate_absolute'
  finish_phase
}

run_intermittent() {
  want_phase intermittent || return 0
  local d port cfg i j
  d="$(phase_dir intermittent)"; next_port; port="$PORT"; cfg="$(make_config "$d" "$port" 0 30s 1)"
  say "case 4. intermittent (partial) encryption — the middle is left in plaintext"
  note "claim: the sample reads both ends, so an overwrite that leaves the middle plaintext"
  note "       is still measured; whole-file entropy is what an attacker would be betting on"
  for i in $(seq 1 40); do
    for j in $(seq 1 600); do printf 'plain text line %s of report %s, ordinary content\n' "$j" "$i"; done > "$d/data/part_$i.txt"
  done
  calibrate "$d" "$cfg" || { bad intermittent "calibration failed"; return 1; }
  start_detector "$cfg" "$d/log/run.log" 30s "$port" || { bad intermittent "the detector never answered /healthz"; return 1; }
  pause_for_demo "$port"
  sleep 3
  note "workload: 8 KiB of each 30 KiB file overwritten with high-entropy bytes, no rename"
  "$PYTHON" "$ROOT/testdata/scenarios/encryptor.py" --path "$d/data" --rate intermittent \
    --partial-bytes 8192 --suffix '' --seed 7 2>&1 | tail -1
  sleep 8
  note "verdicts now held:"; verdicts "$port" | sed 's/^/      /'
  assert_alert intermittent "$d/log/run.log" 2 'entropy_deviation|magic_mismatch'
  finish_phase
}

run_tail() {
  want_phase tail || return 0
  local d port cfg
  d="$(phase_dir tail)"; next_port; port="$PORT"; cfg="$(make_config "$d" "$port" 0 30s 1)"
  say "case 5. tail-only overwrite — the tail sample is read"
  note "claim: an overwrite confined to the end of the file is in the sample, because the"
  note "       sensor reads the head and the tail and never the whole file"
  "$PYTHON" - "$d/data" <<'PY'
import os,sys
d=sys.argv[1]
for i in range(1,31):
    with open(os.path.join(d,"partial_%d.pdf"%i),"wb") as f:
        f.write(b"%PDF-1.4\n"+b"plain text line of the fixture document, nothing unusual here\n"*3200)
PY
  # The baseline has to record what this extension looks like when it is an
  # ordinary document, or the tail overwrite would be compared against pages
  # that were already high-entropy and the deviation would be invisible.
  cat > "$d/warm_pdf.sh" <<'SH'
#!/usr/bin/env bash
"$GRIMA_DEMO_PY" - "$1" <<'PY'
import glob,os,sys
for p in glob.glob(os.path.join(sys.argv[1],"*.pdf")):
    with open(p,"r+b") as f:
        f.seek(0)
        f.write(b"%PDF-1.4\n"+b"plain text line of the fixture document, nothing unusual here\n"*3200)
PY
SH
  calibrate "$d" "$cfg" "bash '$d/warm_pdf.sh' '$d/data'" || { bad tail "calibration failed"; return 1; }
  start_detector "$cfg" "$d/log/run.log" 30s "$port" || { bad tail "the detector never answered /healthz"; return 1; }
  pause_for_demo "$port"
  sleep 3
  note "workload: 30 plaintext .pdf of 256 KiB, last 16 KiB overwritten with high-entropy bytes"
  "$PYTHON" "$ROOT/testdata/scenarios/head_tail_fixture.py" --path "$d/data" --region tail \
    --bytes 16384 --files 30 --size-bytes 262144 --extension .pdf --seed 7 2>&1 | tail -1
  sleep 8
  note "verdicts now held:"; verdicts "$port" | sed 's/^/      /'
  assert_alert tail "$d/log/run.log" 2 'entropy_deviation|magic_mismatch'
  finish_phase
}

run_drip() {
  want_phase drip || return 0
  local d port cfg
  d="$(phase_dir drip)"; next_port; port="$PORT"; cfg="$(make_config "$d" "$port" 0 30s 1)"
  say "case 6. drip encryption — the cumulative track, not the window"
  note "claim: files rewritten one at a time never fill the decaying window, and the"
  note "       irreversible work still accumulates into a verdict"
  note "fixture: 24 high-entropy .dat files of 4 MiB; the workload rewrites and renames each"
  "$PYTHON" "$ROOT/testdata/scenarios/quiet_drip.py" --path "$d/data" --files 24 \
    --size-bytes 4194304 --extension .dat --seed 7 --prepare 2>&1 | tail -1
  # .dat is the content-unchecked extension: the baseline must record it as the
  # high-entropy format it is, so the workload's own rewrites produce no content
  # deviation and the evidence has to come from the cumulative track.
  cat > "$d/warm_dat.sh" <<'SH'
#!/usr/bin/env bash
"$GRIMA_DEMO_PY" - "$1" <<'PY'
import glob,os,random,sys
r=random.Random(11)
for p in glob.glob(os.path.join(sys.argv[1],"*.dat")):
    with open(p,"rb+") as f:
        f.seek(0); f.write(r.randbytes(262144))
PY
SH
  calibrate "$d" "$cfg" "bash '$d/warm_dat.sh' '$d/data'" || { bad drip "calibration failed"; return 1; }
  start_detector "$cfg" "$d/log/run.log" 50s "$port" || { bad drip "the detector never answered /healthz"; return 1; }
  pause_for_demo "$port"
  sleep 3
  note "workload: 24 files, one per second, each renamed to .locked"
  "$PYTHON" "$ROOT/testdata/scenarios/quiet_drip.py" --path "$d/data" --files 24 \
    --size-bytes 4194304 --extension .dat --interval 1 --seed 7 2>&1 | tail -1
  sleep 8
  note "verdicts now held:"; verdicts "$port" | sed 's/^/      /'
  assert_alert drip "$d/log/run.log" 2 'unknown_extension_activity|cum_bytes_rewritten|create_burst|rename_burst'
  finish_phase
}

run_decoy() {
  want_phase decoy || return 0
  local d port cfg canary
  d="$(phase_dir decoy)"; next_port; port="$PORT"; cfg="$(make_config "$d" "$port" 1 30s 1)"
  say "case 7. decoy touch — a canary is a critical override"
  note "claim: no legitimate process has business writing a file the user never created,"
  note "       so the touch sets a floor rather than adding to a score"
  calibrate "$d" "$cfg" || { bad decoy "calibration failed"; return 1; }
  start_detector "$cfg" "$d/log/run.log" 30s "$port" || { bad decoy "the detector never answered /healthz"; return 1; }
  pause_for_demo "$port"
  sleep 3
  canary="$d/data/_grima_canary.doc"
  if [ ! -f "$canary" ]; then
    skip decoy "no canary at $canary (decoy.names or planting changed)"; finish_phase; return 0
  fi
  note "canaries planted: $(ls "$d/data" | grep -cE '^_grima_canary|^quarterly_report|^invoices_backup') in the monitored root"
  note "workload: one append to the planted canary $(basename "$canary")"
  printf 'an encryptor just overwrote the canary\n' >> "$canary"
  sleep 6
  note "verdicts now held:"; verdicts "$port" | sed 's/^/      /'
  note "note: score 0 with level critical is the override contract, not a bug — a rule sets a"
  note "      floor on the level; it does not invent a score out of unnamed evidence"
  if grep -q 'override=R-DECOY-TOUCH' "$d/log/run.log"; then
    ok decoy "$(grep -m1 'override=R-DECOY-TOUCH' "$d/log/run.log" | sed 's/^time=[^ ]* //')"
  else
    bad decoy "the canary was touched but R-DECOY-TOUCH did not fire"
    grep -m2 'ransomware risk detected' "$d/log/run.log"
  fi
  finish_phase
}

run_rules() {
  want_phase rules || return 0
  local d port cfg
  d="$(phase_dir rules)"; next_port; port="$PORT"; cfg="$(make_config "$d" "$port" 0 30s 1)"
  say "case 8. hard rules — raw events, evaluated the instant they arrive"
  note "claim: an anti-recovery command line raises the floor without waiting for a window tick,"
  note "       and cannot be averaged away by benign signals"
  calibrate "$d" "$cfg" || { bad rules "calibration failed"; return 1; }
  start_detector "$cfg" "$d/log/run.log" 45s "$port" || { bad rules "the detector never answered /healthz"; return 1; }
  pause_for_demo "$port"
  sleep 3
  note "workload: a process whose command line is 'vssadmin delete shadows /all /quiet'"
  note "          launched by a parent that then exits, so the offender heads its own tree:"
  note "          overrides attach to a tree root, and evaluate() asks the root for its hits"
  bash -c '
    exec -a "vssadmin delete shadows /all /quiet" sleep 20 &
    sleep 3
  ' &
  sleep 20
  note "verdicts now held:"; verdicts "$port" | sed 's/^/      /'
  if grep -q 'override=R-SHADOW-DELETE' "$d/log/run.log"; then
    ok rules "$(grep -m1 'override=R-SHADOW-DELETE' "$d/log/run.log" | sed 's/^time=[^ ]* //')"
  else
    bad rules "R-SHADOW-DELETE did not fire on the matching command line"
    grep -m2 'ransomware risk detected' "$d/log/run.log"
  fi
  finish_phase
}

# The matcher layer and the pipeline are different claims. The rules below are
# pure predicates and are verified by their own table-driven tests; whether a
# hit reaches the verdict stream is a property of the wiring, and that is what
# the rules phase above exercises end to end.
run_rule_tests() {
  want_phase rule-tests || return 0
  say "case 9. rule predicates — table-driven, happy and sad path each"
  if ( cd "$ROOT" && go test -count=1 -run 'Rule|Shadow|Decoy|Persist|Eventlog|Backup|Usn' ./internal/rules/ 2>&1 | tail -3 ); then
    ok rule-tests "the rule predicates pass their own tests"
  else
    bad rule-tests "the rule tests failed"
  fi
}

run_split() {
  want_phase split || return 0
  local d port cfg workers=8 per=120 size=524288
  d="$(phase_dir split)"; next_port; port="$PORT"
  # Uncalibrated on purpose: the claim is about an absolute threshold, and a
  # short window keeps the in-window count about what the workload is doing now.
  cfg="$(make_config "$d" "$port" 0 2s 0)"
  say "case 10. cooperative split — one workload spread over 8 processes"
  note "claim: work split across cooperating children is still detected as one behaviour"
  note "       (no baseline: with no per-host distribution, the absolute rate is the evidence)"
  note "workload: 8 children contend for one shared pool of $((workers*per)) files,"
  note "          each claiming a file by exclusive create, so no child owns a partition"
  "$PYTHON" "$ROOT/testdata/scenarios/split.py" --path "$d/data" --workers $workers \
    --files-per-worker $per --size $size --ext .txt --claim --prepare 2>&1 | tail -1
  start_detector "$cfg" "$d/log/run.log" 40s "$port" || { bad split "the detector never answered /healthz"; return 1; }
  if ! grep -q 'no usable baseline' "$d/log/run.log"; then
    note "note: the detector did not report running uncalibrated"
  else
    note "detector reports: $(grep -m1 'no usable baseline' "$d/log/run.log" | sed 's/^time=[^ ]* //')"
  fi
  pause_for_demo "$port"
  sleep 3
  "$PYTHON" "$ROOT/testdata/scenarios/split.py" --path "$d/data" --workers $workers \
    --files-per-worker $per --size $size --ext .txt --interval 0.02 --hold 5 --claim 2>&1 | tail -2
  sleep 6
  note "verdicts now held:"; verdicts "$port" | sed 's/^/      /'
  assert_alert split "$d/log/run.log" 2 'write_rate_absolute|write_burst|create_burst'
  finish_phase
}

run_uncalibrated() {
  want_phase uncalibrated || return 0
  local d port cfg i ready signals
  d="$(phase_dir uncalibrated)"; next_port; port="$PORT"; cfg="$(make_config "$d" "$port" 0 30s 0)"
  say "case 11. uncalibrated mode — unknown is not reported as benign"
  note "claim: with no baseline the detector says so, and omits the deviation signals"
  note "       rather than reporting them as zero (which would read as 'nothing happening')"
  rm -f "$d/baseline.json"
  for i in $(seq 1 20); do printf 'report %s\n' "$i" > "$d/data/report_$i.txt"; done
  start_detector "$cfg" "$d/log/run.log" 25s "$port" || { bad uncalibrated "the detector never answered /healthz"; return 1; }
  pause_for_demo "$port"
  sleep 3
  ready="$(curl -fsS "http://127.0.0.1:$port/healthz" | "$PYTHON" -c 'import json,sys; print(json.load(sys.stdin)["calibration_ready"])')"
  note "startup says: $(grep -m1 'no usable baseline' "$d/log/run.log" | sed 's/^time=[^ ]* //' || echo '(no such line)')"
  note "healthz calibration_ready = $ready"
  if grep -q 'id="banner"' <(curl -fsS "http://127.0.0.1:$port/"); then
    note "the dashboard carries the uncalibrated-mode banner element"
  else
    bad uncalibrated "the dashboard has no banner element"; finish_phase; return 1
  fi
  note "workload: the same benign rewrite the calibrated detector stayed silent on"
  for i in $(seq 1 20); do printf 'rewritten %s\n' "$i" > "$d/data/report_$i.txt"; done
  sleep 6
  signals="$(verdicts "$port" | awk '{print $NF}' | tr '\n' ' ')"
  note "signals in the verdicts: ${signals:-none}"
  if [ "$ready" = "False" ] && ! printf '%s' "$signals" | grep -qE 'entropy_deviation|write_burst|rename_burst|delete_rate|dir_fanout'; then
    ok uncalibrated "reported uncalibrated, and no deviation signal was asserted from a missing baseline"
  else
    bad uncalibrated "calibration_ready=$ready, yet a deviation signal was reported"
  fi
  finish_phase
}

run_observability() {
  want_phase observability || return 0
  local d port cfg body
  d="$(phase_dir observability)"; next_port; port="$PORT"; cfg="$(make_config "$d" "$port" 0 30s 1)"
  say "case 12. observability — every bounded queue counts what it lost"
  note "claim: drops, watch failures and unlanded watches are surfaced through /healthz,"
  note "       because a detector that is silently blind is worse than no detector"
  calibrate "$d" "$cfg" || { bad observability "calibration failed"; return 1; }
  start_detector "$cfg" "$d/log/run.log" 20s "$port" || { bad observability "the detector never answered /healthz"; return 1; }
  pause_for_demo "$port"
  sleep 2
  body="$(curl -fsS "http://127.0.0.1:$port/healthz")"
  printf '%s' "$body" | "$PYTHON" -c '
import json,sys
h=json.load(sys.stdin)
print("      %-22s %s" % ("calibration_ready", h["calibration_ready"]))
print("      %-22s %s / %s" % ("bus published / dropped", h["bus_published"], h["bus_dropped"]))
print("      %-22s %s" % ("live processes", h["live_processes"]))
for name,s in sorted(h["sensors"].items()):
    extra=s.get("Extra") or {}
    keys=[k for k in ("watch_failures","add_pending","add_dropped","overflow","rescans","tracked") if k in extra]
    print("      %-22s events=%s dropped=%s %s" % ("sensor "+name, s["Events"], s["Dropped"],
          " ".join("%s=%s"%(k,extra[k]) for k in sorted(keys))))
samples={k.replace("baseline_samples_",""):v for k,v in h.get("extra",{}).items() if k.startswith("baseline_samples")}
print("      %-22s %s" % ("baseline samples", samples))
print("      %-22s %s" % ("history", {k:v for k,v in h.get("extra",{}).items() if k.startswith("history") or k=="sse_dropped"}))
'
  if printf '%s' "$body" | grep -q '"bus_dropped"'; then
    ok observability "health exposes drop counters, per-sensor counters and baseline sample counts"
  else
    bad observability "the health payload is missing the drop counters"
  fi
  finish_phase
}

# ---------------------------------------------------------------------------

printf '\033[1mGRIMA demonstration\033[0m — %s\n' "$(date '+%Y-%m-%d %H:%M')"
note "binary:    $BINARY"
note "work root: $WORK"
note "phases:    $PHASES"
note "python:    $PYTHON"
[ -z "$INTERACTIVE" ] || note "the demo pauses between phases; open the dashboard URL it prints"

for phase in $PHASES; do
  case "$phase" in
    build) run_build ;;
    burst) run_burst ;;
    benign) run_benign ;;
    create-storm) run_create_storm ;;
    intermittent) run_intermittent ;;
    tail) run_tail ;;
    drip) run_drip ;;
    decoy) run_decoy ;;
    rules) run_rules ;;
    rule-tests) run_rule_tests ;;
    split) run_split ;;
    uncalibrated) run_uncalibrated ;;
    observability) run_observability ;;
    *) echo "unknown phase: $phase (see --list)" >&2; record SKIP "$phase" "unknown phase" ;;
  esac
done

say "summary"
printf '    %-14s %-6s %s\n' PHASE RESULT DETAIL
for row in "${RESULTS[@]}"; do
  IFS='|' read -r status phase detail <<<"$row"
  printf '    %-14s %-6s %s\n' "$phase" "$status" "$detail"
done
printf '\n    %d passed, %d failed, %d skipped\n' "$PASSED" "$FAILED" "$SKIPPED"
[ "$FAILED" -eq 0 ] || exit 1
