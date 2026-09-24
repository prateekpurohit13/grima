#!/usr/bin/env python3
"""Score an attribution trace against a known writer.

The detector writes one JSON line per blame decision when GRIMA_ATTRIB_TRACE is
set (see internal/attrib/trace.go). This joins that trace to the PID of a known
writer and reports how often the detector blamed the right process.

Usage:
  score_attribution.py --trace TRACE.jsonl --writer-pid 1234
  score_attribution.py --trace TRACE.jsonl --writer-pid 1234 --json
"""

from __future__ import annotations

import argparse
import json
import sys
from collections import Counter


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Score a GRIMA attribution trace.")
    parser.add_argument("--trace", required=True, help="trace file written by the detector")
    parser.add_argument("--writer-pid", type=int, required=True, help="PID of the known writer")
    parser.add_argument("--json", action="store_true", help="emit a JSON summary instead of text")
    return parser.parse_args()


def load(path: str) -> tuple[list[dict], int]:
    decisions: list[dict] = []
    unreadable = 0
    with open(path, errors="replace") as handle:
        for line in handle:
            line = line.strip()
            if not line:
                continue
            try:
                decisions.append(json.loads(line))
            except json.JSONDecodeError:
                unreadable += 1
    return decisions, unreadable


def summarise(decisions: list[dict], writer_pid: int) -> dict:
    correct = 0
    unattributed = 0
    blamed: Counter[str] = Counter()
    confidence: list[float] = []

    for d in decisions:
        pid = d.get("pid") or 0
        if pid == 0:
            unattributed += 1
        elif pid == writer_pid:
            correct += 1
        else:
            blamed[f"{pid}:{d.get('name') or '?'}"] += 1
        if d.get("confidence") is not None:
            confidence.append(float(d["confidence"]))

    total = len(decisions)
    return {
        "total": total,
        "correct": correct,
        "unattributed": unattributed,
        "misattributed": total - correct - unattributed,
        "accuracy": (correct / total) if total else 0.0,
        "top_blamed": blamed.most_common(5),
        "confidence_mean": (sum(confidence) / len(confidence)) if confidence else 0.0,
        "confidence_max": max(confidence) if confidence else 0.0,
    }


def main() -> int:
    args = parse_args()

    try:
        decisions, unreadable = load(args.trace)
    except FileNotFoundError:
        print(f"error: no trace at {args.trace}", file=sys.stderr)
        return 2

    if not decisions:
        print("error: trace is empty; was GRIMA_ATTRIB_TRACE set on the detector?", file=sys.stderr)
        return 2

    s = summarise(decisions, args.writer_pid)
    s["unreadable_lines"] = unreadable

    if args.json:
        print(json.dumps(s, indent=2))
        return 0

    print(f"writer pid          {args.writer_pid}")
    print(f"decisions           {s['total']}")
    print(f"correct             {s['correct']}")
    print(f"misattributed       {s['misattributed']}")
    print(f"unattributed        {s['unattributed']}")
    print(f"accuracy            {s['accuracy']:.1%}")
    print(f"confidence          mean {s['confidence_mean']:.3f}, max {s['confidence_max']:.3f}")
    if s["top_blamed"]:
        print("blamed instead:")
        for name, count in s["top_blamed"]:
            print(f"  {count:>6}  {name}")
    if unreadable:
        print(f"warning: {unreadable} trace lines were unreadable", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
