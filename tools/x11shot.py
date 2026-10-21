#!/usr/bin/env python3
"""Screenshot an X window by id, without xdotool or xwininfo.

This environment has neither, so the window tree is walked with Xlib through
ctypes. Used by the fidelity probe to capture the page viewport of the
browser window (and not the whole desktop).

Importable: `capture(display, output, title_match)`.
"""
import ctypes
import ctypes.util
import subprocess
import sys
import time

x11 = ctypes.CDLL(ctypes.util.find_library("X11"))


class XWindowAttributes(ctypes.Structure):
    _fields_ = [
        ("x", ctypes.c_int),
        ("y", ctypes.c_int),
        ("width", ctypes.c_int),
        ("height", ctypes.c_int),
        ("border_width", ctypes.c_int),
        ("depth", ctypes.c_int),
        ("visual", ctypes.c_void_p),
        ("root", ctypes.c_ulong),
        ("class_", ctypes.c_int),
        ("bit_gravity", ctypes.c_int),
        ("win_gravity", ctypes.c_int),
        ("backing_store", ctypes.c_int),
        ("backing_planes", ctypes.c_ulong),
        ("backing_pixel", ctypes.c_ulong),
        ("save_under", ctypes.c_int),
        ("colormap", ctypes.c_ulong),
        ("map_installed", ctypes.c_int),
        ("map_state", ctypes.c_int),
        ("all_event_masks", ctypes.c_long),
        ("your_event_mask", ctypes.c_long),
        ("do_not_propagate_mask", ctypes.c_long),
        ("override_redirect", ctypes.c_int),
        ("screen", ctypes.c_void_p),
    ]


x11.XOpenDisplay.restype = ctypes.c_void_p
x11.XOpenDisplay.argtypes = [ctypes.c_char_p]
x11.XDefaultRootWindow.restype = ctypes.c_ulong
x11.XDefaultRootWindow.argtypes = [ctypes.c_void_p]
x11.XQueryTree.restype = ctypes.c_int
x11.XQueryTree.argtypes = [
    ctypes.c_void_p, ctypes.c_ulong, ctypes.POINTER(ctypes.c_ulong),
    ctypes.POINTER(ctypes.c_ulong), ctypes.POINTER(ctypes.POINTER(ctypes.c_ulong)),
    ctypes.POINTER(ctypes.c_uint),
]
x11.XGetWindowAttributes.restype = ctypes.c_int
x11.XGetWindowAttributes.argtypes = [
    ctypes.c_void_p, ctypes.c_ulong, ctypes.POINTER(XWindowAttributes),
]
x11.XFetchName.restype = ctypes.c_int
x11.XFetchName.argtypes = [ctypes.c_void_p, ctypes.c_ulong, ctypes.POINTER(ctypes.c_char_p)]
x11.XFree.argtypes = [ctypes.c_void_p]


def window_name(display, window):
    name = ctypes.c_char_p()
    if x11.XFetchName(display, window, ctypes.byref(name)) and name.value:
        return name.value.decode("utf-8", "replace")
    return ""


def walk(display, window, found):
    root = ctypes.c_ulong()
    parent = ctypes.c_ulong()
    children = ctypes.POINTER(ctypes.c_ulong)()
    count = ctypes.c_uint()
    if not x11.XQueryTree(display, window, ctypes.byref(root), ctypes.byref(parent),
                           ctypes.byref(children), ctypes.byref(count)):
        return
    try:
        for index in range(count.value):
            child = children[index]
            attributes = XWindowAttributes()
            if x11.XGetWindowAttributes(display, child, ctypes.byref(attributes)):
                if (attributes.map_state == 2 and attributes.width > 50
                        and attributes.height > 50):
                    found.append((child, attributes.width, attributes.height,
                                  window_name(display, child)))
            walk(display, child, found)
    finally:
        if children:
            x11.XFree(children)


def find_window(display_name, title_match="", timeout=10.0):
    """Return (window_id, width, height, title) of a mapped window."""
    display = x11.XOpenDisplay(display_name.encode())
    if not display:
        raise RuntimeError(f"cannot open display {display_name}")
    deadline = time.time() + timeout
    windows = []
    while time.time() < deadline:
        windows = []
        walk(display, x11.XDefaultRootWindow(display), windows)
        if title_match:
            matches = [w for w in windows if title_match in w[3]]
            if matches:
                return matches[-1]
        elif windows:
            return windows[-1]
        time.sleep(0.25)
    return None


def capture(display_name, output, title_match="", timeout=10.0):
    found = find_window(display_name, title_match, timeout)
    if not found:
        raise RuntimeError(f"no window found for {title_match!r} on {display_name}")
    window, width, height, name = found
    result = subprocess.run(
        ["import", "-display", display_name, "-window", hex(window), output],
        capture_output=True,
    )
    if result.returncode != 0:
        raise RuntimeError(result.stderr.decode())
    return {"window": hex(window), "width": width, "height": height, "title": name}


def main():
    if len(sys.argv) < 3:
        print("usage: x11shot.py <display> <output.png> [title-match]", file=sys.stderr)
        return 2
    display_name, output = sys.argv[1], sys.argv[2]
    title_match = sys.argv[3] if len(sys.argv) > 3 else ""
    try:
        info = capture(display_name, output, title_match)
    except RuntimeError as err:
        print(str(err), file=sys.stderr)
        return 1
    print(f"saved {output} ({info['width']}x{info['height']}) {info['title']!r}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
