#!/usr/bin/env bash
# Benign workload: bulk media I/O — a video/audio encode when an encoder is
# available, a raw-media generate-and-remux pass when it is not.
#
# Sprint 4 uses this to measure false positives. Media encoding writes hundreds
# of megabytes of incompressible output, which is the closest benign workload to
# the entropy signature of encryption, so a detector that flags it is wrong.
#
# Usage: benign-media-encode.sh [workdir]
#   workdir defaults to $TMPDIR/grima-benign-media
#
# Prints one SUMMARY line at the end: files, bytes, elapsed seconds and which
# path ran (ffmpeg or fallback).
set -uo pipefail

WORK="${1:-${TMPDIR:-/tmp}/grima-benign-media}"
SECONDS_PER_CLIP="${GRIMA_MEDIA_SECONDS:-20}"
FRAMES="${GRIMA_MEDIA_FRAMES:-6}"

if [ "$(command -v ffmpeg)" = "" ]; then
  PYTHON="${GRIMA_PYTHON:-$(command -v python3 || command -v python)}" \
    || { echo "error: no video encoder and no python for the fallback" >&2; exit 2; }
fi

rm -rf "$WORK"
mkdir -p "$WORK/out"

start="$(date +%s)"
files=0
bytes=0
mode="fallback"

echo "media workload: work directory $WORK"

if command -v ffmpeg >/dev/null 2>&1; then
  mode="ffmpeg"
  echo "encoder: $(ffmpeg -version 2>/dev/null | head -1)"
  for round in 1 2 3; do
    clip="$WORK/out/clip.$round.mp4"
    ffmpeg -hide_banner -loglevel error -y \
      -f lavfi -i "testsrc2=size=1280x720:rate=30:duration=$SECONDS_PER_CLIP" \
      -c:v libx264 -preset veryfast -pix_fmt yuv420p "$clip" || exit 1
    size=$(wc -c < "$clip")
    files=$((files + 1))
    bytes=$((bytes + size))
    echo "round $round/3: $clip ($size bytes)"
  done
  audio="$WORK/out/audio.m4a"
  ffmpeg -hide_banner -loglevel error -y \
    -f lavfi -i "sine=frequency=440:duration=$SECONDS_PER_CLIP" -c:a aac "$audio" || exit 1
  size=$(wc -c < "$audio")
  files=$((files + 1))
  bytes=$((bytes + size))
  echo "audio: $audio ($size bytes)"
else
  echo "NOTE: no ffmpeg on PATH; running the raw-media fallback instead"
  # A raw-media workload still exercises the same shape: large incompressible
  # writes. Frames are generated, then remuxed into one container file.
  "$PYTHON" - "$WORK" "$FRAMES" <<'PY'
import os
import sys

work, frames = sys.argv[1], int(sys.argv[2])
out = os.path.join(work, "out")
width, height = 640, 360
frame = bytearray(width * height * 3)
written = []
for i in range(frames):
    for y in range(height):
        base = (y * 3 + i * 7) & 0xFF
        row = bytes(((base + x * 3 + i) & 0xFF) for x in range(width * 3))
        frame[y * width * 3:(y + 1) * width * 3] = row
    path = os.path.join(out, "frame.%d.raw" % i)
    with open(path, "wb") as f:
        f.write(frame)
    written.append(path)
    print("frame %d/%d: %s (%d bytes)" % (i + 1, frames, path, len(frame)))

container = os.path.join(out, "clip.remux")
with open(container, "wb") as dest:
    for path in written:
        with open(path, "rb") as src:
            while True:
                block = src.read(1 << 20)
                if not block:
                    break
                dest.write(block)
print("remux: %s (%d bytes)" % (container, os.path.getsize(container)))
PY
  [ $? -eq 0 ] || exit 1
  for f in "$WORK"/out/*; do
    files=$((files + 1))
    bytes=$((bytes + $(wc -c < "$f")))
  done
fi

elapsed=$(( $(date +%s) - start ))
echo "SUMMARY scenario=media_encode files_written=$files bytes_written=$bytes elapsed_s=$elapsed mode=$mode"
