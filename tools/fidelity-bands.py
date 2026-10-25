#!/usr/bin/env python3
"""fidelity-bands: one-shot Meta 1 band-measurement rig for browser-lite.

Encodes the pinned method from odd/tasks/meta1-codeberg.md:

- L20: 24 equal horizontal bands over grid height G = max(engine height,
  reference content height), white-NW padding of both to 1000xG, per band
  `magick compare -metric AE -fuzz 25%` -> AE and % of band area, plus
  whole-page AE; CSV + markdown table. The Firefox reference is a
  `1000,3600` headless capture trimmed with `magick -fuzz F% -trim`.
- L21: rigid vertical phase-offset (d) scan over a y-window, reporting the
  best d and its AE% (the falsification method); re-binning note (band
  limits move when heights change; AE total and phase scan are robust).
- L22: fuzz-0% trim correction (fuzz 12% eats the live 11px announcement
  strip; fuzz 0% reproduces the pinned 1669) — hence the default --fuzz 0,
  with higher F as the sensitivity mode.

Usage (KISS):
    python3 tools/fidelity-bands.py --engine <bin> --url <url> --out <dir>
        [--fuzz 0] [--phase y0,h[,maxd]] [--firefox-bin firefox] [--dry-run]

All artifacts (PNGs, CSV, table, commands log) go into --out. Exit 0 on
success; clear errors for missing magick/firefox. Stdlib + ImageMagick CLI
+ firefox CLI only, matching tools/fidelity-probe.py conventions.
"""
import argparse
import pathlib
import re
import shlex
import shutil
import subprocess
import sys
import tempfile

REPO = pathlib.Path(__file__).resolve().parent.parent
BANDS = 24
COMPARE_FUZZ = "25%"
VIEWPORT = (1000, 3600)


def band_bounds(grid, index):
    """(y0, y1) of band `index` for the L20 24-band grid over height `grid`."""
    return (index * grid // BANDS, (index + 1) * grid // BANDS)


def parse_compare_output(text):
    """Parse `magick compare -metric AE` stderr `AE (fraction)` (L20).

    Returns (ae_pixels, diff_percent); diff% = changed pixels / band area
    = fraction * 100 since both crops share the band area.
    """
    first = (text or "").strip().splitlines()[0] if (text or "").strip() else ""
    match = re.match(r"\s*([\d.eE+-]+)\s+\(?([\d.eE+-]+)\)?", first)
    if not match:
        raise ValueError(f"cannot parse compare output: {text!r}")
    ae = float(match.group(1))
    return ae, float(match.group(2)) * 100


class Rig:
    def __init__(self, args):
        self.args = args
        self.out = pathlib.Path(args.out)
        self.log = None

    def cmd(self, argv, **kwargs):
        """Run argv, logging the exact command line first."""
        line = shlex.join(str(c) for c in argv)
        print(f"$ {line}", flush=True)
        if self.log is not None:
            self.log.write(line + "\n")
        return subprocess.run(argv, capture_output=True, text=True, **kwargs)

    def check(self, result, what):
        if result.returncode != 0:
            err = (result.stderr or result.stdout or "").strip()[-500:]
            sys.exit(f"fidelity-bands: {what} failed: {err}")
        return result

    # -- capture steps (L20 steps 1-2, L22 fuzz correction) -----------------
    def render_engine(self):
        png = self.out / "engine.png"
        result = self.cmd([self.args.engine, "--png", str(png), self.args.url])
        self.check(result, "engine render")
        return png

    def render_firefox(self):
        raw = self.out / "ff-raw.png"
        profile = self.out / "ff-profile"
        if profile.exists():
            shutil.rmtree(profile)
        profile.mkdir(parents=True)
        (profile / "user.js").write_text(
            'user_pref("layout.css.prefers-color-scheme.content-override", 1);\n',
            encoding="utf-8",
        )
        result = self.cmd(
            [
                self.args.firefox_bin, "--headless", "--no-remote",
                "--screenshot", str(raw),
                "--window-size", f"{VIEWPORT[0]},{VIEWPORT[1]}",
                "--profile", str(profile),
                self.args.url,
            ]
        )
        self.check(result, "firefox capture")
        return raw

    # -- image helpers -------------------------------------------------------
    def identify(self, path):
        result = self.cmd(["magick", "identify", "-format", "%w %h", str(path)])
        self.check(result, f"identify {path}")
        width, height = (int(v) for v in result.stdout.split())
        return width, height

    def compare(self, first, second):
        """(ae, diff_pct) via the L20 pinned `compare -metric AE -fuzz 25%`."""
        result = self.cmd(
            ["magick", "compare", "-metric", "AE",
             "-fuzz", COMPARE_FUZZ, str(first), str(second), "null:"]
        )
        return parse_compare_output(result.stderr or result.stdout)

    def crop(self, src, width, height, x, y, dst):
        result = self.cmd(
            ["magick", str(src), "-crop", f"{width}x{height}+{x}+{y}",
             "+repage", str(dst)]
        )
        self.check(result, f"crop {dst}")

    # -- measurement (L20 steps 2-3) ------------------------------------------
    def trim_reference(self, raw):
        """L22: fuzz-0% trim keeps the live strip; reproduces pinned 1669."""
        ref = self.out / "ff-ref.png"
        result = self.cmd(
            ["magick", str(raw), "-fuzz", f"{self.args.fuzz}%",
             "-trim", "+repage", str(ref)]
        )
        self.check(result, "trim firefox reference")
        return ref

    def pad_grid(self, src, grid):
        dst = self.out / (pathlib.Path(src).stem + "-grid.png")
        result = self.cmd(
            ["magick", str(src), "-background", "white",
             "-gravity", "NorthWest", "-extent", f"1000x{grid}", str(dst)]
        )
        self.check(result, f"pad {dst}")
        return dst

    def band_table(self, go_grid, ff_grid, grid, ref_height):
        rows = []
        with tempfile.TemporaryDirectory() as tmp:
            for i in range(BANDS):
                y0, y1 = band_bounds(grid, i)
                height = y1 - y0
                go_band = pathlib.Path(tmp) / f"go-{i}.png"
                ff_band = pathlib.Path(tmp) / f"ff-{i}.png"
                self.crop(go_grid, 1000, height, 0, y0, go_band)
                self.crop(ff_grid, 1000, height, 0, y0, ff_band)
                ae, pct = self.compare(go_band, ff_band)
                rows.append((i, y0, y1, height, ae, pct))
                print(f"band {i:2d} y={y0:4d}..{y1:4d} h={height:3d}"
                      f" ae={ae:12.2f} diff={pct:6.2f}%", flush=True)
        csv = self.out / "bands.csv"
        csv.write_text(
            "band,y0,y1,height,ae,diff_pct\n"
            + "".join(f"{i},{y0},{y1},{h},{ae:.2f},{pct:.2f}\n"
                      for i, y0, y1, h, ae, pct in rows),
            encoding="utf-8",
        )
        lines = ["| band | y0-y1 | diff% |", "| --- | --- | --- |"]
        lines += [f"| {i} | {y0}-{y1} | {pct:.2f} |"
                  for i, y0, y1, _, _, pct in rows]
        ae, pct = self.compare(go_grid, ff_grid)
        area = 1000 * grid
        lines += [f"| whole page (1000x{grid}) | AE={ae:.0f}/{area} | {pct:.2f} |"]
        print(f"whole page: AE={ae:.0f} diff={pct:.2f}%", flush=True)
        table = self.out / "table.md"
        table.write_text("\n".join(lines) + "\n", encoding="utf-8")
        return rows, (ae, pct)

    def phase_scan(self, go_png, ref_png, ref_width, spec):
        """L21 falsification method: rigid offset d over an FF y-window."""
        parts = [int(v) for v in spec.split(",")]
        if len(parts) not in (2, 3):
            sys.exit("fidelity-bands: --phase wants y0,h[,maxd]")
        y0, height = parts[0], parts[1]
        _, grid = self.identify(go_png)
        maxd = parts[2] if len(parts) == 3 else grid - y0 - height
        if maxd < 0:
            sys.exit("fidelity-bands: --phase window is outside the engine render")
        with tempfile.TemporaryDirectory() as tmp:
            tmp = pathlib.Path(tmp)
            ff_win = tmp / "ff-win.png"
            self.crop(ref_png, ref_width, height, 0, y0, ff_win)
            go_win = tmp / "go-win.png"

            def mismatch(d):
                self.crop(go_png, ref_width, height, 0, y0 + d, go_win)
                _, pct = self.compare(go_win, ff_win)
                return pct

            best_d, best_m, step, d = -1, float("inf"), 10, 0
            curve = []
            while d <= maxd:  # coarse pass, then refine around the best d
                m = mismatch(d)
                curve.append((d, m))
                if m < best_m:
                    best_m, best_d = m, d
                d += step
            for d in range(max(best_d - step, 0), min(best_d + step, maxd) + 1):
                if d % step == 0:
                    continue
                m = mismatch(d)
                curve.append((d, m))
                if m < best_m:
                    best_m, best_d = m, d
            m0 = mismatch(0)
        csv = self.out / "phase.csv"
        csv.write_text("d,mismatch_pct\n"
                       + "".join(f"{d},{m:.2f}\n"
                                 for d, m in sorted(curve)),
                       encoding="utf-8")
        print(f"phase y[{y0},{y0 + height}) best_d={best_d}"
              f" mismatch={best_m:.2f}% (d=0: {m0:.2f}%)", flush=True)
        return best_d, best_m, m0

    def run(self):
        self.out.mkdir(parents=True, exist_ok=True)
        with open(self.out / "commands.log", "w", encoding="utf-8") as log:
            self.log = log
            engine = self.render_engine()
            ff_raw = self.render_firefox()
            ref = self.trim_reference(ff_raw)
            go_w, go_h = self.identify(engine)
            ff_w, ff_h = self.identify(ref)
            print(f"engine {go_w}x{go_h} reference {ff_w}x{ff_h}", flush=True)
            grid = max(go_h, ff_h)  # L20: grid height = max of the two
            go_grid = self.pad_grid(engine, grid)
            ff_grid = self.pad_grid(ref, grid)
            self.band_table(go_grid, ff_grid, grid, ff_h)
            diff = self.out / "diff-annotated.png"
            self.cmd(["magick", "compare", "-metric", "AE",
                      "-fuzz", COMPARE_FUZZ,
                      str(go_grid), str(ff_grid), str(diff)])
            self.cmd(["magick", str(go_grid), str(ff_grid),
                      "+append", str(self.out / "side-by-side.png")])
            if self.args.phase:
                self.phase_scan(engine, ref, min(go_w, ff_w), self.args.phase)
        return 0


def plan(args):
    """The exact command plan printed by --dry-run (pinned by the test)."""
    out = args.out
    lines = [
        f"mkdir -p {out}",
        f"{args.engine} --png {out}/engine.png {args.url}",
        f"{args.firefox_bin} --headless --no-remote --screenshot"
        f" {out}/ff-raw.png --window-size 1000,3600 --profile"
        f" {out}/ff-profile {args.url}",
        f"magick {out}/ff-raw.png -fuzz {args.fuzz}% -trim +repage"
        f" {out}/ff-ref.png",
        "magick identify <png>  # read engine + reference heights, G = max",
        "magick <png> -background white -gravity NorthWest -extent 1000xG"
        " # pad both to the L20 grid",
        "for i in 0..23  # L20: 24 equal bands over G",
        "  magick <grid> -crop 1000xH+0+y +repage <band>  # engine + reference",
        "  magick compare -metric AE -fuzz 25% <engine-band> <ff-band> null:",
        "  # diff% = AE fraction * 100; rows -> bands.csv + table.md",
        "magick compare -metric AE -fuzz 25% <go-grid> <ff-grid>"
        " <diff-annotated.png>  # whole-page AE",
        "magick <go-grid> <ff-grid> +append <side-by-side.png>",
    ]
    if args.phase:
        lines.append(f"# --phase {args.phase}: rigid d scan (L21), best d -> phase.csv")
    return lines


def check_deps(args):
    problems = []
    engine = pathlib.Path(args.engine)
    if not (engine.exists() and engine.stat().st_mode & 0o111):
        problems.append(f"engine binary not found or not executable: {args.engine}")
    if shutil.which(args.firefox_bin) is None:
        problems.append(f"firefox binary not found: {args.firefox_bin}")
    if shutil.which("magick") is None:
        problems.append("magick not found on PATH (ImageMagick 7 required)")
    if problems:
        sys.exit("fidelity-bands: " + "; ".join(problems))


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Meta 1 band-measurement rig (method: odd/tasks/meta1-codeberg.md L20-L22).")
    parser.add_argument("--engine", default=str(REPO / "go" / "browser-lite"),
                        help="engine binary (renders: <bin> --png <out> <url>)")
    parser.add_argument("--url", required=True)
    parser.add_argument("--out", required=True,
                        help="output dir for ALL artifacts (PNGs, CSV, table, log)")
    parser.add_argument("--fuzz", type=float, default=0,
                        help="trim fuzz%% for the Firefox reference (L22: 0 pinned, 12 = sensitivity)")
    parser.add_argument("--phase", default="",
                        help="optional rigid phase scan (L21): y0,h[,maxd] in FF coords")
    parser.add_argument("--firefox-bin", default="firefox")
    parser.add_argument("--dry-run", action="store_true",
                        help="print the exact command plan without executing")
    args = parser.parse_args(argv)
    if args.dry_run:
        print("\n".join(plan(args)))
        return 0
    check_deps(args)
    return Rig(args).run()


if __name__ == "__main__":
    sys.exit(main())
