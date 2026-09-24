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
    parser.add_argument("--prepare", action="store_true",
                        help="rewrite the files in place, no rename and no delay")
    return parser.parse_args()


def payload(size: int, rng: random.Random) -> bytes:
    return rng.randbytes(size)


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

    for i in range(1, args.files + 1):
        path = os.path.join(target, f"blob_{i}{args.extension}")
        with open(path, "wb") as handle:
            handle.write(payload(args.size_bytes, rng))
            handle.flush()
            os.fsync(handle.fileno())
        written += args.size_bytes

        if not args.prepare:
            renamed_path = path + args.suffix
            os.replace(path, renamed_path)
            renamed += 1
            if args.interval:
                time.sleep(args.interval)

    elapsed = time.time() - started
    mode = "prepare" if args.prepare else "drip"
    print(f"SUMMARY fixture=quiet_drip mode={mode} files={args.files} "
          f"bytes_written={written} renamed={renamed} "
          f"interval_s={0 if args.prepare else args.interval} elapsed_s={elapsed:.1f}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
