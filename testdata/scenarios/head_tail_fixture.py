#!/usr/bin/env python3
"""Partial-overwrite fixture for the head+tail entropy sample.

The file sensor reads the first and last `filewatch.entropy_sample_bytes` bytes
of a changed file and computes one Shannon entropy over both halves
(internal/sensor/filewatch). This fixture overwrites only one of those halves,
so a verdict can be attributed to the half that was actually read.

It is a test fixture, not malware: it refuses to touch anything outside --path,
writes only files it created itself, and uses no key material.

Usage:
  head_tail_fixture.py --path DIR --region tail --bytes 4096 --files 5
  head_tail_fixture.py --path DIR --region head --bytes 4096 --files 5

Prints per-file head, tail and combined entropy, then one SUMMARY line. The
detector's sample is the combined pair, so those three numbers say exactly which
half carried the evidence.
"""

from __future__ import annotations

import argparse
import math
import os
import random
import sys

MAGIC = {
    ".pdf": b"%PDF-1.7\n",
    ".docx": b"PK\x03\x04",
    ".xlsx": b"PK\x03\x04",
    ".png": b"\x89PNG\r\n\x1a\n",
    ".txt": b"",
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Partial-overwrite fixture.")
    parser.add_argument("--path", required=True, help="directory to write the fixtures into")
    parser.add_argument("--region", choices=["head", "tail"], default="tail")
    parser.add_argument("--bytes", type=int, default=4096, help="bytes to overwrite")
    parser.add_argument("--sample-bytes", type=int, default=4096,
                        help="bytes the detector samples from each end")
    parser.add_argument("--files", type=int, default=5)
    parser.add_argument("--size-bytes", type=int, default=65536)
    parser.add_argument("--extension", default=".pdf", choices=sorted(MAGIC))
    parser.add_argument("--seed", type=int, default=0, help="0 means use the clock")
    return parser.parse_args()


def plaintext(size: int, extension: str) -> bytes:
    magic = MAGIC[extension]
    filler = []
    line = 0
    total = len(magic)
    while total < size:
        row = ("record %05d ordinary document content for the %s fixture\n" % (line, extension)).encode()
        filler.append(row)
        total += len(row)
        line += 1
    return (magic + b"".join(filler))[:size]


def shannon(data: bytes) -> float:
    if not data:
        return 0.0
    counts = [0] * 256
    for byte in data:
        counts[byte] += 1
    total = len(data)
    return -sum((c / total) * math.log2(c / total) for c in counts if c)


def main() -> int:
    args = parse_args()

    target = os.path.abspath(args.path)
    if not os.path.isdir(target):
        print(f"error: {target} is not a directory", file=sys.stderr)
        return 2
    if args.size_bytes <= args.sample_bytes * 2:
        print("error: --size-bytes must exceed twice --sample-bytes, or the sensor "
              "reads no tail sample at all", file=sys.stderr)
        return 2
    if args.bytes > args.size_bytes:
        print("error: --bytes must not exceed --size-bytes", file=sys.stderr)
        return 2

    rng = random.Random(args.seed or None)
    head_scores: list[float] = []
    tail_scores: list[float] = []
    combined_scores: list[float] = []

    for i in range(1, args.files + 1):
        path = os.path.join(target, f"partial_{i}{args.extension}")
        with open(path, "wb") as handle:
            handle.write(plaintext(args.size_bytes, args.extension))
        offset = 0 if args.region == "head" else args.size_bytes - args.bytes
        with open(path, "r+b") as handle:
            handle.seek(offset)
            handle.write(rng.randbytes(args.bytes))
            handle.flush()
            os.fsync(handle.fileno())

        with open(path, "rb") as handle:
            body = handle.read()
        head = body[: args.sample_bytes]
        tail = body[-args.sample_bytes:]
        combined = head + tail
        h, t, c = shannon(head), shannon(tail), shannon(combined)
        head_scores.append(h)
        tail_scores.append(t)
        combined_scores.append(c)
        print(f"{path}: region={args.region} size={len(body)} "
              f"head_entropy={h:.2f} tail_entropy={t:.2f} sampled_entropy={c:.2f}")

    def mean(values: list[float]) -> float:
        return sum(values) / len(values)

    print(f"SUMMARY fixture=partial_overwrite region={args.region} files={args.files} "
          f"head_mean={mean(head_scores):.2f} tail_mean={mean(tail_scores):.2f} "
          f"sampled_mean={mean(combined_scores):.2f}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
