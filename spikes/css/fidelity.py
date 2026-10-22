#!/usr/bin/env python3
"""Fidelity probe for the css-spike harness (odd/tasks/css-spike.md, S4).

Usage:
    python3 fidelity.py ENGINE.png FIXTURE.html [options]

Captures the same page with Firefox headless at the engine viewport width (the
method of docs/font-svg-fidelity.md), then measures, inside the content box of
the reference:

  - ``ink_share``: share of non-background pixels in the engine render,
  - ``ink_share_ref``: the same share in the reference,
  - ``diff_pct``: share of pixels that differ from the reference with 25% fuzz
    (antialiasing tolerant).

Prints one JSON object. Only the Python 3 standard library and the ImageMagick
CLI are used.
"""

import argparse
import json
import os
import pathlib
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time

# ImageMagick 7 renamed `convert` to `magick` (and `compare` to `magick
# compare`). The old names still work but print a warning and swallow output.
HAS_MAGICK = shutil.which("magick") is not None
MAGICK = ["magick"] if HAS_MAGICK else ["convert"]
COMPARE = ["magick", "compare"] if HAS_MAGICK else ["compare"]

# The reference content box is found with the same 12% fuzz the engine probe
# uses; the pixel diff ignores antialiasing with 25%.
TRIM_FUZZ = 12
DIFF_FUZZ = 25


def run(command, timeout=60):
    return subprocess.run(command, capture_output=True, text=True, timeout=timeout)


def image_size(path):
    result = run([*MAGICK, "identify", "-format", "%w %h", str(path)])
    parts = result.stdout.split()
    if len(parts) != 2:
        return None
    try:
        return int(parts[0]), int(parts[1])
    except ValueError:
        return None


def content_box(path):
    """Bounding box of the non-background content, as ``(w, h, x, y)``.

    ``-trim -format %wx%h+%X+%Y`` prints doubled signs (``728x487++34++45``)
    in ImageMagick 7, so the offsets are parsed tolerantly.
    """
    result = run([
        *MAGICK, str(path), "-fuzz", f"{TRIM_FUZZ}%", "-trim",
        "-format", "%wx%h+%X+%Y", "info:",
    ])
    text = result.stdout.strip().replace(" ", "")
    match = re.match(r"(\d+)x(\d+)(.*)$", text)
    if not match:
        return None
    width, height = int(match.group(1)), int(match.group(2))
    offsets = [abs(int(value)) for value in re.findall(r"-?\d+", match.group(3))]
    x, y = (offsets + [0, 0])[:2]
    if width < 4 or height < 4:
        return None
    return width, height, x, y


def clip_box(box, size):
    """Intersect a content box with an image, so both crops are valid."""
    width, height, x, y = box
    image_width, image_height = size
    x = min(x, image_width)
    y = min(y, image_height)
    width = min(width, image_width - x)
    height = min(height, image_height - y)
    if width < 4 or height < 4:
        return None
    return width, height, x, y


def crop(path, box, output):
    """Crop to the box and pad with white if the source is smaller."""
    width, height, x, y = box
    run([*MAGICK, str(path), "-crop", f"{width}x{height}+{x}+{y}", "+repage", str(output)])
    if not output.is_file():
        return False
    if image_size(output) != (width, height):
        # The cropped canvas can come out smaller at the edges; extend it so
        # both sides of the diff always have the same size.
        run([
            *MAGICK, str(output), "-background", "white", "-gravity", "northwest",
            "-extent", f"{width}x{height}", "+repage", str(output),
        ])
    return image_size(output) == (width, height)


def ink_share(path):
    """Fraction of pixels that are not (near) background."""
    result = run([
        *MAGICK, str(path), "-fuzz", f"{TRIM_FUZZ}%", "-fill", "white",
        "-opaque", "white", "-colorspace", "Gray",
        "-format", "%[fx:1-mean] %k", "info:",
    ])
    parts = result.stdout.split()
    if not parts:
        return None
    try:
        return round(float(parts[0]), 4)
    except ValueError:
        return None


def difference_percent(ours_crop, reference_crop, width, height):
    """Share of differing pixels, ignoring small antialiasing differences."""
    result = run([
        *COMPARE, "-metric", "AE", "-fuzz", f"{DIFF_FUZZ}%",
        str(ours_crop), str(reference_crop), "null:",
    ])
    text = (result.stderr or result.stdout).strip()
    match = re.match(r"([-+]?\d*\.?\d+(?:[eE][-+]?\d+)?)", text)
    if not match:
        return None
    try:
        differ = float(match.group(1))
    except ValueError:
        return None
    return round(differ / (width * height) * 100, 2)


def capture_firefox(fixture, output, width, height, firefox, timeout):
    """Screenshot the fixture with Firefox headless at the given viewport."""
    output.parent.mkdir(parents=True, exist_ok=True)
    profile = pathlib.Path(tempfile.mkdtemp(prefix="ff-profile-", dir=str(output.parent)))
    started = time.time()
    command = [
        firefox, "--headless", "--screenshot", str(output),
        "--window-size", f"{width},{height}", "--profile", str(profile),
        fixture.as_uri(),
    ]
    environment = dict(os.environ, MOZ_HEADLESS="1")
    process = subprocess.Popen(
        command,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
        env=environment,
        start_new_session=True,
    )
    error = ""
    try:
        _, error = process.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        # Kill by PID (and the process group it owns), never by command line.
        try:
            os.killpg(os.getpgid(process.pid), signal.SIGKILL)
        except (ProcessLookupError, PermissionError):
            process.kill()
        try:
            _, error = process.communicate(timeout=10)
        except subprocess.TimeoutExpired:
            error = ""
        return {"ok": False, "error": f"firefox timed out after {timeout:.0f}s"}
    finally:
        shutil.rmtree(profile, ignore_errors=True)

    if process.returncode != 0:
        lines = (error or "").strip().splitlines()
        detail = lines[-1][:400] if lines else f"firefox exited {process.returncode}"
        return {"ok": False, "error": detail}
    if not output.is_file() or output.stat().st_mtime < started - 2:
        return {"ok": False, "error": "firefox produced no screenshot"}
    return {"ok": True}


def main():
    parser = argparse.ArgumentParser(description="Engine vs Firefox fidelity inside the content box.")
    parser.add_argument("engine", help="PNG rendered by the spike")
    parser.add_argument("fixture", help="HTML fixture rendered in the PNG")
    parser.add_argument("--width", type=int, default=800, help="viewport width (default 800)")
    parser.add_argument("--height", type=int, default=1200, help="reference viewport height (default 1200)")
    parser.add_argument("--out", help="directory for the reference and crops (default: a temp dir)")
    parser.add_argument("--reference", help="reuse an existing Firefox reference PNG")
    parser.add_argument("--firefox", default=os.environ.get("FIREFOX", "firefox"))
    parser.add_argument("--timeout", type=float, default=120.0, help="Firefox timeout in seconds")
    args = parser.parse_args()

    engine = pathlib.Path(args.engine).resolve()
    fixture = pathlib.Path(args.fixture).resolve()
    if args.out:
        out = pathlib.Path(args.out).resolve()
    else:
        out = pathlib.Path(tempfile.mkdtemp(prefix="css-spike-fidelity-"))
    out.mkdir(parents=True, exist_ok=True)

    result = {
        "engine": str(engine),
        "fixture": str(fixture),
        "reference": None,
        "view_width": args.width,
        "view_height": args.height,
        "content_box": None,
        "ink_share": None,
        "ink_share_ref": None,
        "diff_pct": None,
        "ok": False,
    }

    def fail(message):
        result["error"] = message
        print(json.dumps(result))
        print(f"fidelity: {message}", file=sys.stderr)
        return 1

    if not engine.is_file():
        return fail(f"engine image not found: {engine}")
    if not fixture.is_file():
        return fail(f"fixture not found: {fixture}")

    if args.reference:
        reference = pathlib.Path(args.reference).resolve()
        if not reference.is_file():
            return fail(f"reference not found: {reference}")
    else:
        reference = out / f"{fixture.stem}-ref.png"
        capture = capture_firefox(
            fixture, reference, args.width, args.height, args.firefox, args.timeout
        )
        if not capture["ok"]:
            return fail(capture["error"])
    result["reference"] = str(reference)

    reference_size = image_size(reference)
    engine_size = image_size(engine)
    if reference_size is None:
        return fail(f"cannot read reference image: {reference}")
    if engine_size is None:
        return fail(f"cannot read engine image: {engine}")

    box = content_box(reference)
    if box is None:
        return fail("the reference has no content box (blank page?)")
    box = clip_box(box, reference_size)
    if box is None:
        return fail("the reference content box falls outside the image")
    box = clip_box(box, engine_size)
    if box is None:
        return fail("the engine image does not intersect the reference content box")
    width, height, x, y = box
    result["content_box"] = f"{width}x{height}+{x}+{y}"

    ours_crop = out / f"{engine.stem}-engine-crop.png"
    reference_crop = out / f"{engine.stem}-ref-crop.png"
    if not crop(engine, box, ours_crop):
        return fail(f"cannot crop the engine image to {result['content_box']}")
    if not crop(reference, box, reference_crop):
        return fail(f"cannot crop the reference to {result['content_box']}")

    result["ink_share"] = ink_share(ours_crop)
    result["ink_share_ref"] = ink_share(reference_crop)
    result["diff_pct"] = difference_percent(ours_crop, reference_crop, width, height)
    result["ok"] = all(
        value is not None
        for value in (result["ink_share"], result["ink_share_ref"], result["diff_pct"])
    )

    print(json.dumps(result))
    if not result["ok"]:
        print("fidelity: could not compute all metrics", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
