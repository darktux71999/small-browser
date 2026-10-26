#!/usr/bin/env python3
"""wpt-score: scoreboard over browser-lite --wpt JSON results.

Reads the JSON contract that --wpt prints (see test/wpt-hook.js header)
and renders a per-slice scoreboard as a markdown table -- the regression
compass for standards conformance.

Input: one or more files, each either
  - JSONL with {"test": name, "exit": N, "result": {...}} lines
    (one line per --wpt run), or
  - a single result object {"harness": ..., "subtests": [...]}
    (use --test NAME to name it, default: the file stem).

Output: a markdown table (stdout, or --out FILE) with one row per test
plus a total row, counting PASS/FAIL/TIMEOUT/NOTRUN/PRECONDITION_FAILED
subtests and carrying the harness status. Exit 0 always: a scoreboard
is a compass, not a gate.

Usage:
    python3 tools/wpt-score.py results.jsonl
    python3 tools/wpt-score.py run1.json run2.json --test smoke
    browser-lite --wpt URL ... | python3 tools/wpt-score.py --stdin
    python3 tools/wpt-score.py results.jsonl --out scores.md
"""
import argparse
import json
import sys
from pathlib import Path

STATUSES = ("PASS", "FAIL", "TIMEOUT", "NOTRUN", "PRECONDITION_FAILED")


def empty_counts():
    """Zeroed per-status counter plus the harness slot."""
    return {"harness": "NONE", **{s: 0 for s in STATUSES}}


def tally(result):
    """Fold one result object into (counts, unknown_statuses)."""
    counts = empty_counts()
    counts["harness"] = result.get("harness", "NONE")
    unknown = []
    for sub in result.get("subtests", []):
        status = sub.get("status", "FAIL")
        if status in STATUSES:
            counts[status] += 1
        else:
            unknown.append(status)
    return counts, unknown


def load_file(path, test_name=None):
    """Yield (test_name, result) pairs from a results file."""
    text = Path(path).read_text(encoding="utf-8").strip()
    if not text:
        return
    try:
        obj = json.loads(text)
    except json.JSONDecodeError:
        obj = None
    if isinstance(obj, dict) and ("subtests" in obj or "result" in obj):
        if "subtests" in obj:
            yield test_name or Path(path).stem, obj
            return
        yield obj.get("test", test_name or Path(path).stem), obj["result"]
        return
    if isinstance(obj, dict):
        raise ValueError(f"{path}: JSON object has neither subtests nor result")
    for lineno, line in enumerate(text.splitlines(), 1):
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except json.JSONDecodeError as exc:
            raise ValueError(f"{path}:{lineno}: invalid JSON: {exc}") from exc
        yield obj.get("test", f"{path}:{lineno}"), obj.get("result") or {}


def render(rows):
    """Render [(test, counts)] as a markdown table plus a totals row."""
    total = empty_counts()
    total["harness"] = "-"
    lines = [
        "| Test | Harness | Total | Pass | Fail | Timeout | Not-run | Precond |",
        "|------|---------|-------|------|------|---------|---------|---------|",
    ]
    for name, counts in rows:
        subtotal = sum(counts[s] for s in STATUSES)
        lines.append(
            "| `%s` | %s | %d | %d | %d | %d | %d | %d |"
            % (name, counts["harness"], subtotal, *(counts[s] for s in STATUSES))
        )
        for key in ("harness", *STATUSES):
            if key != "harness":
                total[key] += counts[key]
    subtotal = sum(total[s] for s in STATUSES)
    lines.append(
        "| **Total** | **%d files** | **%d** | **%d** | **%d** | **%d** | **%d** | **%d** |"
        % (len(rows), subtotal, *(total[s] for s in STATUSES))
    )
    return "\n".join(lines) + "\n"


def main(argv=None):
    """CLI entry point: parse inputs, print the scoreboard."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("files", nargs="*", help="results files (JSONL or single result JSON)")
    parser.add_argument("--stdin", action="store_true", help="read one results file from stdin")
    parser.add_argument("--test", default=None, help="name for a single result object input")
    parser.add_argument("--out", default=None, help="write the table to FILE instead of stdout")
    args = parser.parse_args(argv)

    inputs = list(args.files)
    if args.stdin:
        inputs.append("/dev/stdin")
    if not inputs:
        parser.error("no input: give results files or --stdin")

    rows = []
    warnings = []
    for path in inputs:
        try:
            pairs = list(load_file(path, args.test))
        except (OSError, ValueError) as exc:
            print(f"wpt-score: {exc}", file=sys.stderr)
            return 2
        if not pairs:
            warnings.append(f"{path}: no results")
        for name, result in pairs:
            counts, unknown = tally(result)
            rows.append((name, counts))
            for status in unknown:
                warnings.append(f"{name}: unknown status {status!r} (excluded from buckets)")

    rows.sort()
    table = render(rows)
    if args.out:
        Path(args.out).write_text(table, encoding="utf-8")
    else:
        sys.stdout.write(table)
    for warning in warnings:
        print(f"wpt-score: warning: {warning}", file=sys.stderr)
    passing = sum(c["PASS"] for _, c in rows)
    total = sum(sum(c[s] for s in STATUSES) for _, c in rows)
    print(f"wpt-score: {passing}/{total} subtests passing across {len(rows)} files", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
