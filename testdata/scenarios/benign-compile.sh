#!/usr/bin/env bash
# Benign workload: compile a generated C project, round after round.
#
# Sprint 4 uses this to measure false positives. A build is legitimate bulk I/O
# — hundreds of small object writes plus a handful of large links — and a
# detector that flags it is wrong.
#
# Usage: benign-compile.sh [workdir]
#   workdir defaults to $TMPDIR/grima-benign-compile
#
# Prints one SUMMARY line at the end: files, bytes and elapsed seconds.
set -uo pipefail

WORK="${1:-${TMPDIR:-/tmp}/grima-benign-compile}"
UNITS="${GRIMA_COMPILE_UNITS:-40}"
ROUNDS="${GRIMA_COMPILE_ROUNDS:-3}"

CC=""
for candidate in cc gcc clang; do
  if command -v "$candidate" >/dev/null 2>&1; then
    CC="$candidate"
    break
  fi
done
if [ -z "$CC" ]; then
  echo "error: no C compiler (cc, gcc, clang) on PATH" >&2
  exit 2
fi

SRC="$WORK/src"
OUT="$WORK/out"
rm -rf "$WORK"
mkdir -p "$SRC" "$OUT"

echo "compile workload: $UNITS units x $ROUNDS rounds with $CC"
echo "work directory: $WORK"

cat > "$SRC/common.h" <<'EOF'
#ifndef GRIMA_BENIGN_COMMON_H
#define GRIMA_BENIGN_COMMON_H

static inline int mix(int a, int b) {
    return (a * 31 + b) ^ (a >> 3);
}

static inline long fold(long acc, int value) {
    return mix((int)(acc & 0xffff), value) + (acc >> 1);
}

#endif
EOF

for i in $(seq 1 "$UNITS"); do
  {
    printf '#include "common.h"\n\n'
    printf 'static int table_%s[64] = {' "$i"
    for j in $(seq 1 63); do printf '%s,' "$((i * j % 251))"; done
    printf '0};\n\n'
    printf 'long unit_%s(int seed) {\n    long acc = seed;\n' "$i"
    for j in $(seq 1 12); do
      printf '    acc = fold(acc, table_%s[(%s + seed) & 63]);\n' "$i" "$j"
    done
    printf '    return acc;\n}\n'
  } > "$SRC/unit_$i.c"
done

cat > "$SRC/main.c" <<'EOF'
#include <stdio.h>
#include "common.h"
EOF
for i in $(seq 1 "$UNITS"); do
  printf 'long unit_%s(int);\n' "$i" >> "$SRC/main.c"
done
{
  printf '\nint main(void) {\n    long acc = 7;\n'
  for i in $(seq 1 "$UNITS"); do
    printf '    acc += unit_%s((int)acc);\n' "$i"
  done
  printf '    printf("%%ld\\n", acc);\n    return 0;\n}\n'
} >> "$SRC/main.c"

start="$(date +%s)"
files=0
bytes=0
for round in $(seq 1 "$ROUNDS"); do
  objects=""
  for i in $(seq 1 "$UNITS"); do
    obj="$OUT/unit_$i.$round.o"
    "$CC" -O2 -c "$SRC/unit_$i.c" -o "$obj" || exit 1
    files=$((files + 1))
    bytes=$((bytes + $(wc -c < "$obj")))
    objects="$objects $obj"
  done
  main_obj="$OUT/main.$round.o"
  "$CC" -O2 -c "$SRC/main.c" -o "$main_obj" || exit 1
  # shellcheck disable=SC2086
  "$CC" -O2 -o "$OUT/app.$round" $objects "$main_obj" || exit 1
  files=$((files + 2))
  bytes=$((bytes + $(wc -c < "$main_obj") + $(wc -c < "$OUT/app.$round")))
  echo "round $round/$ROUNDS: $((UNITS + 2)) outputs"
done

elapsed=$(( $(date +%s) - start ))
echo "SUMMARY scenario=compile files_written=$files bytes_written=$bytes elapsed_s=$elapsed compiler=$CC"
