#!/usr/bin/env bash
# Benign workload: a dense batch of atomic saves.
#
# An atomic save writes a temporary file and renames it over the target. Editors,
# config managers, archive extractors and `mv`-based deploy scripts all do this,
# and they do it in bursts. It is the legitimate pattern most similar to
# ransomware's write-then-rename sequence, so it is the workload most likely to
# trip a rename-chain signal.
#
# Usage: benign-atomic-save.sh [workdir]
#   workdir defaults to $TMPDIR/grima-benign-atomic-save
#
# Runs unattended, is idempotent (each run overwrites the same targets with the
# same content), and prints one SUMMARY line: files, bytes, renames, seconds.
set -uo pipefail

WORK="${1:-${TMPDIR:-${TEMP:-${TMP:-/tmp}}}/grima-benign-atomic-save}"
DIRS="${GRIMA_ATOMIC_DIRS:-8}"
FILES="${GRIMA_ATOMIC_FILES:-60}"

mkdir -p "$WORK/tree"

echo "atomic-save workload: $DIRS dirs x $FILES files, temp-then-rename batches"
echo "work directory: $WORK"

start="$(date +%s)"
files=0
bytes=0
renames=0
round=0

for d in $(seq 1 "$DIRS"); do
  dir="$WORK/tree/config-$d"
  mkdir -p "$dir"
  for f in $(seq 1 "$FILES"); do
    target="$dir/service_$f.conf"
    round=$((round + 1))
    if [ $((round % 3)) -eq 0 ]; then
      tmp="$dir/.service_$f.conf.swp"
    else
      tmp="$target.tmp"
    fi
    {
      printf '# service %s/%s configuration\n' "$d" "$f"
      printf 'listen = 127.0.0.1:%s\n' "$((8000 + f))"
      printf 'workers = %s\n' "$((f % 16 + 1))"
      printf 'log_level = info\n'
      printf 'retention_days = %s\n' "$((f % 30 + 1))"
    } > "$tmp"
    files=$((files + 1))
    bytes=$((bytes + $(wc -c < "$tmp")))
    mv -f "$tmp" "$target"
    renames=$((renames + 1))
  done
done

elapsed=$(( $(date +%s) - start ))
echo "SUMMARY scenario=atomic_save files_written=$files bytes_written=$bytes renames=$renames elapsed_s=$elapsed"
