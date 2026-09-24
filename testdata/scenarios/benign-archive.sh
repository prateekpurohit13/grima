#!/usr/bin/env bash
# Benign workload: build a source tree and archive/compress it, repeatedly.
#
# Sprint 4 uses this to measure false positives. Compression writes one large
# high-entropy output per run, which is exactly the shape an entropy-based
# ransomware signal reacts to, so this workload is a real test of it.
#
# Usage: benign-archive.sh [workdir]
#   workdir defaults to $TMPDIR/grima-benign-archive
#
# Prints one SUMMARY line at the end: files, bytes and elapsed seconds.
set -uo pipefail

WORK="${1:-${TMPDIR:-/tmp}/grima-benign-archive}"
DIRS="${GRIMA_ARCHIVE_DIRS:-12}"
FILES_PER_DIR="${GRIMA_ARCHIVE_FILES:-25}"
ROUNDS="${GRIMA_ARCHIVE_ROUNDS:-3}"

TAR=""
for candidate in tar bsdtar; do
  if command -v "$candidate" >/dev/null 2>&1; then
    TAR="$candidate"
    break
  fi
done
GZIP="$(command -v gzip || true)"
if [ -z "$TAR" ] && [ -z "$GZIP" ]; then
  echo "error: no archiver (tar, bsdtar, gzip) on PATH" >&2
  exit 2
fi

SRC="$WORK/source"
OUT="$WORK/archives"
rm -rf "$WORK"
mkdir -p "$OUT"

echo "archive workload: $DIRS dirs x $FILES_PER_DIR files x $ROUNDS rounds"
echo "work directory: $WORK"

start="$(date +%s)"
files=0
bytes=0

for d in $(seq 1 "$DIRS"); do
  mkdir -p "$SRC/sub-$d/logs"
  for f in $(seq 1 "$FILES_PER_DIR"); do
    {
      printf 'directory %s file %s\n' "$d" "$f"
      for line in $(seq 1 200); do
        printf 'record %s-%s-%s ordinary log content for archival\n' "$d" "$f" "$line"
      done
    } > "$SRC/sub-$d/logs/entry_$f.log"
    files=$((files + 1))
    bytes=$((bytes + $(wc -c < "$SRC/sub-$d/logs/entry_$f.log")))
  done
done

for round in $(seq 1 "$ROUNDS"); do
  if [ -n "$TAR" ]; then
    archive="$OUT/tree.$round.tar.gz"
    "$TAR" -czf "$archive" -C "$WORK" source || exit 1
    size=$(wc -c < "$archive")
    files=$((files + 1))
    bytes=$((bytes + size))
    echo "round $round/$ROUNDS: $archive ($size bytes)"
  fi
  if [ -n "$GZIP" ]; then
    plain="$OUT/blob.$round.gz"
    if [ -n "$TAR" ]; then
      cat "$SRC"/sub-*/logs/*.log | "$GZIP" -c > "$plain"
    else
      "$GZIP" -c "$SRC/sub-1/logs/entry_1.log" > "$plain"
    fi
    size=$(wc -c < "$plain")
    files=$((files + 1))
    bytes=$((bytes + size))
    echo "round $round/$ROUNDS: $plain ($size bytes)"
  fi
done

elapsed=$(( $(date +%s) - start ))
echo "SUMMARY scenario=archive files_written=$files bytes_written=$bytes elapsed_s=$elapsed archiver=${TAR:-none}"
