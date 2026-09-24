#!/usr/bin/env bash
# Benign workload: an npm-install-shaped write pattern — many small files, a
# few temp-then-rename pairs, and one large lockfile built up in chunks.
#
# Sprint 4 uses this to measure false positives. Installing a dependency tree is
# legitimate bulk small-file I/O, and it is the closest benign workload to a
# ransomware-shaped write pattern, so a detector that flags it is wrong.
#
# Usage: benign-npm-install.sh [workdir]
#   workdir defaults to $TMPDIR/grima-benign-npm-install
#
# Prints one SUMMARY line at the end: files, bytes and elapsed seconds.
set -uo pipefail

WORK="${1:-${TMPDIR:-${TEMP:-${TMP:-/tmp}}}/grima-benign-npm-install}"
PKGS="${GRIMA_NPM_PACKAGES:-200}"
MODULES="${GRIMA_NPM_MODULES:-6}"

rm -rf "$WORK"
mkdir -p "$WORK/node_modules"

echo "npm-install workload: $PKGS packages x $MODULES modules"
echo "work directory: $WORK"

start="$(date +%s)"
files=0
bytes=0
renames_failed=0

write_file() {
  printf '%s' "$2" > "$1"
  files=$((files + 1))
  bytes=$((bytes + $(wc -c < "$1")))
}

for p in $(seq 1 "$PKGS"); do
  dir="$WORK/node_modules/pkg-$p"
  mkdir -p "$dir/lib"
  write_file "$dir/package.json" "{\"name\":\"pkg-$p\",\"version\":\"1.0.$((p % 40))\",\"main\":\"index.js\",\"license\":\"MIT\",\"description\":\"benign fixture package $p with a description long enough to look like a real registry entry\"}"
  write_file "$dir/index.js" "$(printf 'const lib = require("./lib/mod_0");\nmodule.exports = function pkg%s(x) {\n  return lib(x) + %s;\n};\n' "$p" "$p")"
  write_file "$dir/README.md" "$(printf '# pkg-%s\n\nA fixture package.\n\n## Usage\n\n    const pkg = require("pkg-%s");\n' "$p" "$p")"
  for m in $(seq 1 "$MODULES"); do
    write_file "$dir/lib/mod_$m.js" "$(printf 'module.exports = function mod_%s_%s(v) {\n  return (v * %s) ^ %s;\n};\n' "$p" "$m" "$m" "$p")"
  done
done

# npm extracts each package to a temporary name and renames it into place, so
# the tail of an install is a rename burst over files it has just written.
for p in $(seq 1 20); do
  dir="$WORK/node_modules/pkg-$p"
  for m in $(seq 1 3); do
    printf 'module.exports = function rewritten_%s_%s(v) { return v + %s; };\n' "$p" "$m" "$p" > "$dir/lib/mod_$m.js.tmp"
    if mv "$dir/lib/mod_$m.js.tmp" "$dir/lib/mod_$m.js" 2>> "$WORK/rename-errors.log"; then
      files=$((files + 1))
      bytes=$((bytes + $(wc -c < "$dir/lib/mod_$m.js")))
    else
      renames_failed=$((renames_failed + 1))
    fi
  done
done

# The lockfile is one large file appended to in chunks, the way npm builds it.
lock="$WORK/package-lock.json"
{
  printf '{\n  "name": "benign-fixture",\n  "lockfileVersion": 3,\n  "packages": {\n'
  for p in $(seq 1 "$PKGS"); do
    printf '    "node_modules/pkg-%s": {"version": "1.0.%s", "resolved": "https://registry.npmjs.org/pkg-%s/-/pkg-%s-1.0.%s.tgz", "integrity": "sha512-%s"},\n' \
      "$p" "$((p % 40))" "$p" "$p" "$((p % 40))" "$(printf 'A%.0s' $(seq 1 20))$p"
  done
  printf '  }\n}\n'
} >> "$lock"
files=$((files + 1))
bytes=$((bytes + $(wc -c < "$lock")))

elapsed=$(( $(date +%s) - start ))
echo "SUMMARY scenario=npm_install files_written=$files bytes_written=$bytes elapsed_s=$elapsed packages=$PKGS renames_failed=$renames_failed"
