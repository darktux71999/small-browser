#!/usr/bin/env python3
"""Fidelity probe: render every fixture in test/data/probe-*.html with
blitz-lite and with a reference browser, and report how close they are.

The probe never touches the engine: it only opens pages and takes
screenshots.

    python3 tools/fidelity-probe.py --out /tmp/opencode/probe

Reference: Firefox in headless mode (it is the only full browser in this
environment). The legacy `browser-lite` engine can be used instead with
--reference legacy.
"""
import argparse
import json
import os
import pathlib
import re
import subprocess
import sys
import time

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import x11shot  # noqa: E402

REPO = pathlib.Path(__file__).resolve().parent.parent

# ImageMagick 7 renamed `convert` to `magick`; the old name still works but
# prints a deprecation warning and swallows the -format output.
MAGICK = "magick" if subprocess.run(["which", "magick"], capture_output=True).returncode == 0 else "convert"


def run(command, **kwargs):
    return subprocess.run(command, capture_output=True, text=True, **kwargs)


def title_of(path):
    match = re.search(r"<title>(.*?)</title>", path.read_text(encoding="utf-8"), re.S)
    return match.group(1).strip() if match else path.stem


def image_size(path):
    result = run(["identify", "-format", "%w %h", str(path)])
    parts = result.stdout.split()
    if len(parts) != 2:
        return None
    return int(parts[0]), int(parts[1])


def ink_metrics(path):
    """Fraction of pixels that are not background, and the number of colours."""
    result = run([
        MAGICK, str(path), "-fuzz", "12%", "-fill", "white", "-opaque", "white",
        "-colorspace", "Gray", "-format", "%[fx:1-mean] %k", "info:",
    ])
    fields = result.stdout.split()
    if len(fields) != 2:
        return None, None
    return float(fields[0]), int(fields[1])


def capture_blitz(binary, fixture, display, output, timeout=25.0):
    """Open the fixture in blitz-lite and screenshot its window."""
    process = subprocess.Popen(
        [binary, str(fixture)],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
        env={**os.environ, "DISPLAY": display},
    )
    try:
        time.sleep(1.5)
        info = x11shot.capture(display, str(output), title_of(fixture), timeout=timeout)
        return info
    except RuntimeError as err:
        return {"error": str(err)}
    finally:
        try:
            os.killpg(os.getpgid(process.pid), 15)
        except (ProcessLookupError, PermissionError):
            process.terminate()
        process.wait(timeout=10)


def capture_firefox(fixture, output, width, height, timeout=120):
    profile = pathlib.Path("/tmp/opencode/probe/firefox-profile")
    profile.mkdir(parents=True, exist_ok=True)
    result = run([
        "firefox", "--headless", "--screenshot", str(output),
        "--window-size", f"{width},{height}", "--profile", str(profile),
        fixture.resolve().as_uri(),
    ], timeout=timeout)
    if result.returncode != 0:
        return {"error": result.stderr[-400:]}
    return {"ok": True}


def capture_legacy(fixture, output, timeout=60):
    result = run([
        "/home/darktux/.cache/bl-target/debug/browser-lite", "--png", str(output),
        str(fixture),
    ], timeout=timeout)
    if result.returncode != 0:
        return {"error": result.stderr[-400:]}
    return {"ok": True}


def content_box(path):
    """The bounding box of the non-background content of an image."""
    result = run([
        MAGICK, str(path), "-fuzz", "12%", "-trim", "-format", "%wx%h+%X+%Y", "info:",
    ])
    match = re.match(r"(\d+)x(\d+)\+?(\d*)\+?(\d*)", result.stdout.strip())
    if not match:
        return None
    width, height, x, y = (int(value or 0) for value in match.groups())
    if width < 4 or height < 4:
        return None
    return width, height, x, y


def difference_percent(ours_crop, reference_crop):
    """Share of pixels that differ, ignoring small antialiasing differences."""
    result = run([
        "compare", "-metric", "AE", "-fuzz", "25%", str(ours_crop), str(reference_crop), "null:",
    ])
    text = (result.stderr or result.stdout).strip()
    match = re.match(r"([\d.]+)", text)
    if not match:
        return None
    area = image_size(ours_crop)
    if not area:
        return None
    return round(float(match.group(1)) / (area[0] * area[1]) * 100, 2)


def side_by_side(ours, reference, output):
    """A reference/ours pair, for the report."""
    run([MAGICK, str(reference), str(ours), "+append", str(output)])


def compare(ours, reference):
    """RMSE over the content area only.

    The reference decides the region: the page of the browser window shows
    the desktop below the content, and that would drown the comparison.
    """
    box = content_box(reference)
    if box is None:
        return None, None
    width, height, x, y = box
    geometry = f"{width}x{height}+{x}+{y}"
    ours_crop = ours.with_suffix(".crop.png")
    reference_crop = reference.with_suffix(".crop.png")
    run([MAGICK, str(ours), "-crop", geometry, "+repage", str(ours_crop)])
    run([MAGICK, str(reference), "-crop", geometry, "+repage", str(reference_crop)])
    result = run(["compare", "-metric", "RMSE", str(ours_crop), str(reference_crop), "null:"])
    text = (result.stderr or result.stdout).strip()
    match = re.search(r"\(([\d.]+)\)", text)
    rmse = float(match.group(1)) if match else None
    ours_ink, ours_colors = ink_metrics(ours_crop)
    reference_ink, reference_colors = ink_metrics(reference_crop)
    return rmse, {
        "box": geometry,
        "ours_ink": round(ours_ink, 4) if ours_ink is not None else None,
        "ours_colors": ours_colors,
        "ref_ink": round(reference_ink, 4) if reference_ink is not None else None,
        "ref_colors": reference_colors,
        "diff_pct": difference_percent(ours_crop, reference_crop),
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--fixtures", default=str(REPO / "test" / "data"))
    parser.add_argument("--out", default="/tmp/opencode/probe")
    parser.add_argument("--binary", default="/home/darktux/.cache/bl-target/debug/blitz-lite")
    parser.add_argument("--display", default=":1")
    parser.add_argument("--reference", choices=["firefox", "legacy", "none"], default="firefox")
    parser.add_argument("--only", default="")
    parser.add_argument("--skip-capture", action="store_true",
                        help="reuse the screenshots already in the output directory")
    args = parser.parse_args()

    out = pathlib.Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    fixtures = sorted(pathlib.Path(args.fixtures).glob("probe-*.html"))
    if args.only:
        fixtures = [f for f in fixtures if args.only in f.name]

    rows = []
    for fixture in fixtures:
        name = fixture.stem
        ours = out / f"{name}-blitz.png"
        reference = out / f"{name}-ref.png"
        print(f"== {name}", flush=True)

        if args.skip_capture and ours.exists() and reference.exists():
            size = image_size(ours)
            info = {"width": size[0], "height": size[1]}
            ref_info = {"ok": True}
        else:
            info = capture_blitz(args.binary, fixture, args.display, ours)
            if "error" in info:
                print(f"   blitz: {info['error']}", flush=True)
                rows.append({"fixture": name, "error": info["error"]})
                continue
            print(f"   blitz: {info['width']}x{info['height']}", flush=True)

            ref_info = {"skipped": True}
            if args.reference == "firefox":
                ref_info = capture_firefox(fixture, reference, info["width"], info["height"])
            elif args.reference == "legacy":
                ref_info = capture_legacy(fixture, reference)

        row = {
            "fixture": name,
            "title": title_of(fixture),
            "size": f"{info['width']}x{info['height']}",
        }
        if ref_info.get("ok"):
            size = image_size(reference)
            if size and size != (info["width"], info["height"]):
                run([MAGICK, str(reference), "-crop",
                     f"{info['width']}x{info['height']}+0+0", "+repage", str(reference)])
            rmse, detail = compare(ours, reference)
            row["rmse"] = rmse
            row.update(detail or {})
            side_by_side(ours, reference, out / f"{name}-side-by-side.png")
        else:
            ours_ink, ours_colors = ink_metrics(ours)
            row["ours_ink"] = round(ours_ink, 4) if ours_ink is not None else None
            row["ours_colors"] = ours_colors
        rows.append(row)
        print(f"   {json.dumps(row)}", flush=True)

    report = out / "report.json"
    report.write_text(json.dumps(rows, indent=2), encoding="utf-8")
    print(f"\nwrote {report}")


if __name__ == "__main__":
    main()
