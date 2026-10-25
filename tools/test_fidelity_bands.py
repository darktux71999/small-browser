#!/usr/bin/env python3
"""Tests for tools/fidelity-bands.py.

Conventions: stdlib unittest only (the repo has no test runner for scripts,
so `python3 tools/test_fidelity_bands.py` is the runner). The end-to-end run
needs network + firefox + a built engine, so everything here is offline:
pure-function pins plus a --dry-run golden check.
"""
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

TOOLS = Path(__file__).resolve().parent

import importlib.util

_spec = importlib.util.spec_from_file_location(
    "fidelity_bands", TOOLS / "fidelity-bands.py")
fb = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(fb)


class BandBoundsTest(unittest.TestCase):
    """band_bounds covers the 24-band grid of L20 for any grid height."""

    def test_bounds(self):
        cases = [
            # (grid height, band index, expected (y0, y1))
            (2297, 0, (0, 95)),
            (2297, 23, (2201, 2297)),
            (2297, 11, (1052, 1148)),
            (1942, 0, (0, 80)),
            (1942, 23, (1861, 1942)),
            (1669, 5, (347, 417)),
            (2400, 0, (0, 100)),
            (2400, 23, (2300, 2400)),
        ]
        for grid, index, want in cases:
            with self.subTest(grid=grid, index=index):
                self.assertEqual(fb.band_bounds(grid, index), want)

    def test_bands_tile_grid(self):
        for grid in (1669, 1942, 2297, 3175):
            with self.subTest(grid=grid):
                total = sum(
                    y1 - y0 for y0, y1 in (fb.band_bounds(grid, i) for i in range(24))
                )
                self.assertEqual(total, grid)


class ParseCompareTest(unittest.TestCase):
    """compare outputs `AE (fraction)`; diff% = fraction*100 (L20)."""

    def test_parse(self):
        cases = [
            ("7676.23 (0.0808)", (7676.23, 8.08)),
            ("326661 (0.142212)", (326661.0, 14.22)),
            ("0 (0)", (0.0, 0.0)),
        ]
        for raw, want in cases:
            with self.subTest(raw=raw):
                ae, pct = fb.parse_compare_output(raw)
                self.assertAlmostEqual(ae, want[0])
                self.assertAlmostEqual(pct, want[1], places=2)


class DryRunTest(unittest.TestCase):
    """--dry-run prints the exact command plan without executing anything."""

    def run_tool(self, *args):
        return subprocess.run(
            [sys.executable, str(TOOLS / "fidelity-bands.py"), *args],
            capture_output=True,
            text=True,
        )

    def test_help(self):
        result = self.run_tool("--help")
        self.assertEqual(result.returncode, 0)
        self.assertIn("--engine", result.stdout)
        self.assertIn("--url", result.stdout)
        self.assertIn("--out", result.stdout)
        self.assertIn("--fuzz", result.stdout)
        self.assertIn("--phase", result.stdout)

    def test_dry_run_plan(self):
        with tempfile.TemporaryDirectory() as tmp:
            out = str(Path(tmp) / "m1")
            result = self.run_tool(
                "--engine", "/tmp/bl-m3/browser-lite",
                "--url", "https://codeberg.org",
                "--out", out,
                "--dry-run",
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            lines = result.stdout.splitlines()
            # Exact golden prefix: the deterministic capture phase.
            self.assertEqual(lines[0], f"mkdir -p {out}")
            self.assertEqual(
                lines[1],
                f"/tmp/bl-m3/browser-lite --png {out}/engine.png https://codeberg.org",
            )
            self.assertEqual(
                lines[2],
                f"firefox --headless --no-remote --screenshot {out}/ff-raw.png"
                f" --window-size 1000,3600 --profile {out}/ff-profile"
                " https://codeberg.org",
            )
            self.assertEqual(
                lines[3],
                f"magick {out}/ff-raw.png -fuzz 0% -trim +repage {out}/ff-ref.png",
            )
            # Band loop is templated on the measured grid height G.
            self.assertIn("for i in 0..23", result.stdout)
            self.assertIn("magick compare -metric AE -fuzz 25%", result.stdout)
            # Nothing executed: the out dir must not exist after a dry run.
            self.assertFalse(Path(out).exists(), "dry run must not create --out")

    def test_missing_firefox_is_a_clear_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            out = str(Path(tmp) / "m1")
            result = self.run_tool(
                "--engine", "/bin/true",
                "--url", "https://codeberg.org",
                "--out", out,
                "--firefox-bin", "/nonexistent/firefox",
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("firefox", result.stderr.lower())


if __name__ == "__main__":
    unittest.main()
