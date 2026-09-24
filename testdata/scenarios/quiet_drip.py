#!/usr/bin/env python3
"""Window-quiet drip fixture.

`encryptor.py --rate drip` is the sprint's named drip case, but every extension
it will touch (.txt, .log, .csv, .json and the magic-bearing formats) is checked
for content, so its first file trips a window signal and the cumulative track
cannot be observed on its own.

This fixture writes the same pattern to files whose content and extension carry
no window evidence: `.dat` files that already hold high-entropy bytes, so
overwriting them changes neither their entropy nor their (unchecked) header.
The only fresh evidence a run produces is therefore cumulative — writes to a
`.locked` extension the host has never seen, plus bytes rewritten — which is
exactly the state the dual-track design claims is caught and the decaying window
is claimed to miss.

This is a test fixture, not malware: it uses no key material, cannot decrypt
anything, refuses to touch anything outside --path, writes only `<name>.dat`
files, and renames nothing but those files.

Usage:
  quiet_drip.py --path DIR --prepare            # seed/extend, rewrite in place
  quiet_drip.py --path DIR --files 24 --interval 5   # the drip case
  quiet_drip.py --path DIR --files 24 --interval 2.5 --write-ms 2500

Prints one SUMMARY line naming what it did.
"""

from __future__ import annotations

import argparse
import os
import random
import sys
import time

DEFAULT_SIZE = 4 << 20


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Window-quiet drip fixture.")
    parser.add_argument("--path", required=True, help="directory to operate inside")
    parser.add_argument("--files", type=int, default=24)
    parser.add_argument("--size-bytes", type=int, default=DEFAULT_SIZE)
    parser.add_argument("--interval", type=float, default=5.0,
                        help="seconds between files in drip mode")
    parser.add_argument("--suffix", default=".locked")
    parser.add_argument("--extension", default=".dat",
                        help="extension whose content is not checked by the sensor")
    parser.add_argument("--seed", type=int, default=0, help="0 means use the clock")
    parser.add_argument("--write-ms", type=int, default=0,
                        help="spread each file's write over this many milliseconds")
    parser.add_argument("--prepare", action="store_true",
                        help="rewrite the files in place, no rename and no delay")
    return parser.parse_args()


def write_file(path: str, size: int, rng: random.Random, spread_ms: int) -> None:
    """Write size random bytes, optionally spread over spread_ms milliseconds.

    A single write is the normal case. Spreading one is how a large file lands
    on a slow disk, and it also keeps the writing process visible to
    correlation attribution for longer than its two-second window, which is the
    difference between the cumulative counters accumulating on one fingerprint
    and being scattered across whichever processes were busy instead.
    """
    with open(path, "wb") as handle:
        if spread_ms <= 0:
            handle.write(rng.randbytes(size))
            handle.flush()
            os.fsync(handle.fileno())
            return
        chunks = 8
        step = max(1, size // chunks)
        pause = (spread_ms / 1000.0) / chunks
        remaining = size
        while remaining > 0:
            count = min(step, remaining)
            handle.write(rng.randbytes(count))
            handle.flush()
            remaining -= count
            if remaining:
                time.sleep(pause)
        os.fsync(handle.fileno())


def rename_over(path: str, suffix: str, attempts: int = 20) -> bool:
    """Rename path to path+suffix, retrying a sharing violation.

    A file sensor that samples a file immediately after a write holds a read
    handle on Windows for a moment, and Windows denies a rename while a handle
    without FILE_SHARE_DELETE is open. The detector does exactly that, so a
    single os.replace is racy on this platform; retrying keeps the fixture's
    shape without changing what the detector observes.
    """
    target = path + suffix
    pause = 0.02
    for _ in range(attempts):
        try:
            os.replace(path, target)
            return True
        except OSError:
            time.sleep(pause)
            pause = min(pause * 1.6, 0.2)
    return False


def main() -> int:
    args = parse_args()

    target = os.path.abspath(args.path)
    if not os.path.isdir(target):
        print(f"error: {target} is not a directory", file=sys.stderr)
        return 2

    rng = random.Random(args.seed or None)
    started = time.time()
    written = 0
    renamed = 0
    skipped = 0

    spread = 0 if args.prepare else args.write_ms
    for i in range(1, args.files + 1):
        path = os.path.join(target, f"blob_{i}{args.extension}")
        write_file(path, args.size_bytes, rng, spread)
        written += args.size_bytes

        if not args.prepare:
            if rename_over(path, args.suffix):
                renamed += 1
            else:
                skipped += 1
                print(f"warning: rename blocked for {path} after retries", file=sys.stderr)
            if args.interval:
                time.sleep(args.interval)

    elapsed = time.time() - started
    mode = "prepare" if args.prepare else "drip"
    print(f"SUMMARY fixture=quiet_drip mode={mode} files={args.files} "
          f"bytes_written={written} renamed={renamed} skipped={skipped} "
          f"write_ms={spread} interval_s={0 if args.prepare else args.interval} "
          f"elapsed_s={elapsed:.1f}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
