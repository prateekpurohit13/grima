#!/usr/bin/env python3
"""Known-writer scenario for measuring GRIMA's attribution accuracy.

This is a test fixture, not malware. It uses no key material, cannot decrypt
anything, refuses to touch anything outside --path, and removes only files whose
name starts with attrib_. It prints its own PID and process name so the
detector's blame can be compared against ground truth.

It exists because the Phase 0 smoke test blamed firefox.exe for an encryptor's
writes. Measuring how often that happens needs a writer whose PID is known and a
competing process whose write volume is known.

Two roles:

  writer      remove its own earlier output, seed N files of exactly --size
              bytes, wait --settle seconds so the process sensor samples the
              seeding, then rewrite each file at --interval seconds per file
  background  keep rewriting one large file at a steady rate, to compete with
              the writer for the attributor's attention

Usage:
  attribution.py --role writer --path DIR [--files 65] [--size 4096]
                 [--interval 0.1] [--suffix .locked] [--settle 2.0] [--seed N]
  attribution.py --role background --path DIR [--size 4194304]
                 [--interval 0.2] [--duration 20]

Output is one key=value line per event, flushed as it is written, each ending
with the wall-clock time it was written so a harness can line the phases up with
the detector's own decision trace:

  pid=1234 name=python.exe role=writer t=1758666900.123
  phase=clean removed=0 t=1758666900.130
  phase=seed files=65 bytes=266240 t=1758666900.480
  phase=ready t=1758666900.481
  phase=rewrite files=65 t=1758666902.482
  phase=done rewritten=65 skipped=0 t=1758666909.010

Score a run by setting GRIMA_ATTRIB_TRACE to a file: each line of that trace is
one file event the detector attributed, and every event in the writer's window
belongs to this process.
"""

from __future__ import annotations

import argparse
import os
import random
import sys
import time

PREFIX = "attrib_"
BACKGROUND_FILE = PREFIX + "background.bin"

PLAIN_LINE = b"attrib fixture report line, ordinary content\n"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Known-writer fixture for GRIMA attribution.")
    parser.add_argument("--role", choices=["writer", "background"], default="writer")
    parser.add_argument("--path", required=True, help="directory to operate inside")
    parser.add_argument("--files", type=int, default=65, help="files the writer seeds and rewrites")
    parser.add_argument("--size", type=int, default=4096, help="bytes per file")
    parser.add_argument("--interval", type=float, default=0.1,
                        help="seconds between files, 0 for a burst")
    parser.add_argument("--suffix", default=".locked", help="extension to rename to (empty to skip)")
    parser.add_argument("--settle", type=float, default=2.0,
                        help="seconds to wait after seeding, longer than the sample window")
    parser.add_argument("--duration", type=float, default=20.0,
                        help="seconds the background writer runs for")
    parser.add_argument("--seed", type=int, default=0, help="0 means use the clock")
    return parser.parse_args()


def say(line: str) -> None:
    print(f"{line} t={time.time():.3f}", flush=True)


def announce(role: str) -> None:
    name = os.path.basename(sys.executable)
    say(f"pid={os.getpid()} name={name} role={role}")


def seed_payload(size: int) -> bytes:
    filled = (PLAIN_LINE * (size // len(PLAIN_LINE) + 1))[:size]
    return filled


def clean(path: str) -> int:
    removed = 0
    for name in os.listdir(path):
        if not name.startswith(PREFIX):
            continue
        target = os.path.join(path, name)
        if not os.path.isfile(target):
            continue
        try:
            os.remove(target)
            removed += 1
        except OSError as exc:
            say(f"skip {target}: {exc}")
    return removed


def seed(path: str, files: int, size: int) -> int:
    payload = seed_payload(size)
    written = 0
    for i in range(1, files + 1):
        target = os.path.join(path, f"{PREFIX}{i:03d}.txt")
        try:
            with open(target, "wb") as handle:
                handle.write(payload)
                handle.flush()
                os.fsync(handle.fileno())
        except OSError as exc:
            say(f"skip {target}: {exc}")
            continue
        written += 1
    return written


def rewrite(path: str, name: str, payload: bytes) -> bool:
    target = os.path.join(path, name)
    try:
        with open(target, "r+b") as handle:
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
    except OSError as exc:
        say(f"skip {target}: {exc}")
        return False
    return True


def rename_to_suffix(path: str, name: str, suffix: str) -> None:
    if not suffix:
        return
    try:
        os.replace(os.path.join(path, name), os.path.join(path, name + suffix))
    except OSError as exc:
        say(f"skip {name}{suffix}: {exc}")


def run_writer(args: argparse.Namespace, rng: random.Random) -> int:
    say(f"phase=clean removed={clean(args.path)}")

    seeded = seed(args.path, args.files, args.size)
    say(f"phase=seed files={seeded} bytes={seeded * args.size}")
    if seeded == 0:
        say("error: nothing could be seeded")
        return 2

    say("phase=ready")
    time.sleep(args.settle)

    say(f"phase=rewrite files={seeded}")
    rewritten = 0
    for i in range(1, seeded + 1):
        name = f"{PREFIX}{i:03d}.txt"
        if rewrite(args.path, name, rng.randbytes(args.size)):
            rename_to_suffix(args.path, name, args.suffix)
            rewritten += 1
        if args.interval:
            time.sleep(args.interval)

    say(f"phase=done rewritten={rewritten} skipped={seeded - rewritten}")
    return 0


def run_background(args: argparse.Namespace, rng: random.Random) -> int:
    target = os.path.join(args.path, BACKGROUND_FILE)
    payload = rng.randbytes(args.size)

    try:
        handle = open(target, "wb")
    except OSError as exc:
        say(f"error: {target}: {exc}")
        return 2

    say(f"phase=start size={args.size} interval={args.interval} duration={args.duration}")
    rounds = 0
    deadline = time.monotonic() + args.duration
    with handle:
        while time.monotonic() < deadline:
            handle.seek(0)
            handle.write(payload)
            handle.flush()
            rounds += 1
            if args.interval:
                time.sleep(args.interval)

    say(f"phase=done rounds={rounds} bytes={rounds * args.size}")
    return 0


def main() -> int:
    args = parse_args()

    target = os.path.abspath(args.path)
    if not os.path.isdir(target):
        print(f"error: {target} is not a directory", file=sys.stderr)
        return 2
    if args.size <= 0 or args.files <= 0:
        print("error: --size and --files must be positive", file=sys.stderr)
        return 2

    args.path = target
    announce(args.role)

    rng = random.Random(args.seed or None)
    if args.role == "writer":
        return run_writer(args, rng)
    return run_background(args, rng)


if __name__ == "__main__":
    sys.exit(main())
