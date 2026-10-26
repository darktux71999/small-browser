#!/usr/bin/env python3
"""Tests for tools/wpt-score.py.

Conventions: stdlib unittest only (the repo has no test runner for
scripts, so `python3 tools/test_wpt_score.py` is the runner). All cases
are offline: tally/render pins plus canned-JSON load checks.
"""
import importlib.util
import json
import unittest
from pathlib import Path

TOOLS = Path(__file__).resolve().parent

_spec = importlib.util.spec_from_file_location(
    "wpt_score", TOOLS / "wpt-score.py")
ws = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(ws)


def result(subs, harness="OK", message=None):
    """Build a scorer-contract result object."""
    return {
        "harness": harness,
        "message": message,
        "subtests": [
            {"name": n, "status": s, "message": m}
            for n, s, m in subs
        ],
    }


CANNED_JSONL = "\n".join([
    json.dumps({"test": "/a.html", "exit": 0, "result": result([
        ("one", "PASS", None), ("two", "PASS", None)])}),
    json.dumps({"test": "/b.html", "exit": 1, "result": result([
        ("one", "PASS", None), ("two", "FAIL", "nope"),
        ("three", "TIMEOUT", None)], harness="OK")}),
    json.dumps({"test": "/c.html", "exit": 1, "result": result(
        [], harness="ERROR", message="no subtests recorded")}),
]) + "\n"


class TallyTest(unittest.TestCase):
    """tally folds one result object into per-status counts."""

    def test_counts(self):
        counts, unknown = ws.tally(result([
            ("a", "PASS", None), ("b", "FAIL", "x"),
            ("c", "TIMEOUT", None), ("d", "NOTRUN", None),
            ("e", "PRECONDITION_FAILED", None)]))
        self.assertEqual(counts,
                         {"harness": "OK", "PASS": 1, "FAIL": 1,
                          "TIMEOUT": 1, "NOTRUN": 1,
                          "PRECONDITION_FAILED": 1})
        self.assertEqual(unknown, [])

    def test_harness_carried(self):
        counts, _ = ws.tally(result([], harness="ERROR", message="boom"))
        self.assertEqual(counts["harness"], "ERROR")

    def test_unknown_status_reported(self):
        counts, unknown = ws.tally(result([("a", "BOGUS", None)]))
        self.assertEqual(unknown, ["BOGUS"])
        self.assertEqual(sum(counts[s] for s in ws.STATUSES), 0)


class RenderTest(unittest.TestCase):
    """render emits the markdown scoreboard with a totals row."""

    def test_table(self):
        rows = [("/a.html", {**{s: 0 for s in ws.STATUSES},
                             "harness": "OK", "PASS": 2}),
                ("/b.html", {**{s: 0 for s in ws.STATUSES},
                             "harness": "OK", "PASS": 1, "FAIL": 1,
                             "TIMEOUT": 1})]
        table = ws.render(rows)
        self.assertIn("| `/a.html` | OK | 2 | 2 | 0 | 0 | 0 | 0 |", table)
        self.assertIn("| `/b.html` | OK | 3 | 1 | 1 | 1 | 0 | 0 |", table)
        self.assertIn("| **Total** | **2 files** | **5** | **3** | **1** | **1** | **0** | **0** |",
                      table)


class LoadTest(unittest.TestCase):
    """load_file accepts JSONL, single results, and wrapped lines."""

    def write(self, text, name="r.jsonl"):
        path = Path(self._tmp.name) / name
        path.write_text(text, encoding="utf-8")
        return str(path)

    def setUp(self):
        import tempfile
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)

    def test_jsonl(self):
        pairs = list(ws.load_file(self.write(CANNED_JSONL)))
        self.assertEqual([n for n, _ in pairs],
                         ["/a.html", "/b.html", "/c.html"])
        self.assertEqual(pairs[1][1]["subtests"][1]["status"], "FAIL")

    def test_single_result(self):
        path = self.write(json.dumps(result([("x", "PASS", None)])),
                           "one.json")
        pairs = list(ws.load_file(path, test_name="smoke"))
        self.assertEqual(len(pairs), 1)
        self.assertEqual(pairs[0][0], "smoke")

    def test_bad_json_errors(self):
        with self.assertRaises(ValueError):
            list(ws.load_file(self.write("{nope")))


if __name__ == "__main__":
    unittest.main()
