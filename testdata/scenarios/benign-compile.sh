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

WORK="${1:-${TMPDIR:-${TEMP:-${TMP:-/tmp}}}/grima-benign-compile}"
UNITS="${GRIMA_COMPILE_UNITS:-40}"
ROUNDS="${GRIMA_COMPILE_ROUNDS:-3}"

CC=""
for candidate in cc gcc clang; do
  if command -v "$candidate" >/dev/null 2>&1; then
    CC="$candidate"
    break
  fi
done
GO=""
if [ -z "$CC" ] && command -v go >/dev/null 2>&1; then
  GO=go
fi
if [ -z "$CC" ] && [ -z "$GO" ]; then
  echo "error: no compiler on PATH (cc, gcc, clang, go)" >&2
  exit 2
fi

SRC="$WORK/src"
OUT="$WORK/out"
rm -rf "$WORK"
mkdir -p "$SRC" "$OUT"

if [ -z "$CC" ]; then
  # No C toolchain: build a generated Go program instead, with a private build
  # cache inside the work directory so the compiler's own I/O is in the corpus.
  echo "compile workload: $ROUNDS rounds with $GO (no C compiler on PATH)"
  echo "work directory: $WORK"
  CACHE="$WORK/cache"
  mkdir -p "$CACHE" "$WORK/module"
  cat > "$WORK/module/go.mod" <<'EOF'
module benignfixture

go 1.26
EOF
  cat > "$WORK/module/main.go" <<'EOF'
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	sum := sha256.Sum256([]byte(strings.Repeat("benign", 64)))
	doc := map[string]any{"digest": fmt.Sprintf("%x", sum), "at": time.Now().Format(time.RFC3339)}
	out, _ := json.Marshal(doc)
	_ = http.StatusOK
	_ = tar.TypeReg
	_ = gzip.BestCompression
	fmt.Println(os.Args[0], string(out)[:16])
}
EOF
  start="$(date +%s)"
  files=0
  bytes=0
  export GOCACHE="$CACHE"
  export GOFLAGS=-mod=mod
  for round in $(seq 1 "$ROUNDS"); do
    ( cd "$WORK/module" && "$GO" build -o "$OUT/app.$round" . ) || exit 1
    files=$((files + 1))
    bytes=$((bytes + $(wc -c < "$OUT/app.$round")))
    echo "round $round/$ROUNDS: $OUT/app.$round"
  done
  files=$((files + 2))
  if command -v du >/dev/null 2>&1; then
    bytes=$((bytes + $(du -sk "$CACHE" | cut -f1) * 1024))
  fi
  elapsed=$(( $(date +%s) - start ))
  echo "SUMMARY scenario=compile files_written=$files bytes_written=$bytes elapsed_s=$elapsed compiler=$GO"
  exit 0
fi

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
