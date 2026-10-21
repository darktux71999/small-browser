# Fidelity probe: SVG and fonts

What the engine draws compared to a full browser, measured with screenshots.

The probe never touches the engine: it opens pages in `blitz-lite`, takes a
screenshot of the window, renders the same file with Firefox in headless
mode, and compares the two. It is a diagnostic, not a test suite: a fixture
that fails here is a gap in the engine, and the point of the corpus is to
keep the list of gaps short and dated.

## How to run it

```sh
cd /home/darktux/browser-lite
python3 tools/fidelity-probe.py --out /tmp/opencode/probe
```

It writes, for every fixture, `NAME-blitz.png`, `NAME-ref.png`,
`NAME-side-by-side.png` and a `report.json` with the numbers. Useful flags:
`--only <substring>` to run one fixture, `--skip-capture` to recompute the
metrics from the screenshots already in the output directory, and
`--reference legacy` to compare against the old cairo engine instead of
Firefox.

`tools/x11shot.py` does the window capture on its own: this environment has
no `xdotool` or `xwininfo`, so the window tree is walked through Xlib.

## Method

- The window is 1044x1036. The browser window is captured with
  `import -window <id>`, so the screenshot is the page and not the desktop.
- Firefox renders the same file with `--headless --screenshot` at the same
  size, over a `file://` URL, so relative resources resolve the same way.
- The comparison region is the bounding box of the content **in the
  reference**: the region where the page actually has something. The
  numbers below are computed inside that box.
- `ink` is the share of pixels that are not background, and `diff` is the
  share of pixels that differ from the reference, ignoring small
  antialiasing differences (25% fuzz).

A fixture where `ink` is much lower than the reference means content is
missing. A fixture where `ink` matches and `diff` is high means the content
is there but placed or shaped differently.

## The corpus

| Fixture | What it probes |
| --- | --- |
| `probe-svg-shapes.html` | inline `<svg>`: rect, circle, ellipse, line, polygon, stroke |
| `probe-svg-path.html` | `<path>`: straight and curved, fill, stroke, dash array, line join and cap |
| `probe-svg-gradient.html` | `linearGradient` and `radialGradient` with stops |
| `probe-svg-text.html` | `<text>`, `text-anchor`, and a `transform` group |
| `probe-svg-img.html` | an SVG file as `<img src>` and as a CSS `background-image` |
| `probe-font-stack.html` | named, generic and missing families, and a fallback stack |
| `probe-font-weights.html` | `font-weight` 300 to 900 and `font-style: italic` |
| `probe-font-size.html` | `font-size` in px, em, rem, %, keywords, and `line-height` |
| `probe-font-spacing.html` | `letter-spacing`, `word-spacing`, `text-indent`, `text-align`, `text-transform` |
| `probe-font-fallback.html` | glyphs the family does not have: CJK, accents, symbols |
| `probe-font-webfont.html` | `@font-face` with `local()` and with a `file://` URL |
| `probe-font-ligatures.html` | `font-variant-ligatures` and `font-feature-settings` |

The machine only has one family installed (JetBrains Mono, plus its Nerd
Font build), so font *selection* cannot be compared here: every family,
generic or named, resolves to the same file. What can be compared is
whether a *different* file is used, which is what the `@font-face` fixture
does.

## Results

Measured on 2026-10-21 with the CPU renderer (the GPU path traps on this
machine's NVK driver).

| Fixture | ink (ours) | ink (reference) | diff | Verdict |
| --- | --- | --- | --- | --- |
| probe-svg-shapes | 0.011 | 0.076 | 10% | **nothing is drawn** |
| probe-svg-path | 0.016 | 0.075 | 4% | **nothing is drawn** |
| probe-svg-gradient | 0.010 | 0.149 | 36% | **nothing is drawn** |
| probe-svg-text | 0.015 | 0.032 | 5% | **nothing is drawn** |
| probe-svg-img | 0.026 | 0.109 | 11% | **empty box, no image** |
| probe-font-stack | 0.061 | 0.051 | 8% | matches |
| probe-font-weights | 0.123 | 0.117 | 19% | matches |
| probe-font-size | 0.068 | 0.062 | 10% | matches, except `line-height` |
| probe-font-spacing | 0.041 | 0.037 | 6% | mostly matches |
| probe-font-fallback | 0.085 | 0.076 | 11% | matches |
| probe-font-webfont | 0.107 | 0.110 | 14% | `@font-face` file url ignored |
| probe-font-ligatures | 0.078 | 0.071 | 10% | ligature applied, `font-variant-ligatures` ignored |

The `diff` column stays around 10% even where the rendering is right: the
text is not rasterised pixel by pixel like Firefox does it, and the vertical
rhythm is different (see below), so the glyphs do not land on the same
pixels.

## What works

- **Font size**: px, em, rem, percentages and the `small`/`large` keywords
  all scale the text as the reference does.
- **Font weight**: 300, 400, 500, 700 and 900 pick the same faces as
  Firefox, and the bold and black faces are the real ones from the family,
  not a synthetic bold.
- **Font style**: `italic` is slanted like the reference.
- **`letter-spacing` and `word-spacing`**: applied.
- **`text-align`**: left, centre and right.
- **Fallback**: a glyph the family does not have is drawn as a missing
  glyph box, exactly like Firefox, and the rest of the line is not
  disturbed. The accented Latin characters (`café naïve Grüße`) are correct.
- **OpenType features**: the Nerd Font programming ligature (`=>` drawn as
  `⇒`) is applied, and it looks the same as in Firefox.
- **Backgrounds**: the page background covers the whole window, with no
  desktop showing through.

## What does not work

### SVG is not rendered at all

All five SVG fixtures render as an empty area: the `<svg>` element is
treated as an element with no rendering, so shapes, paths, gradients and
`<text>` inside an inline SVG are all invisible. An SVG file used as
`<img src>` leaves an empty box (not even the alternative text), and the
same file as a CSS `background-image` is not painted either. This is the
biggest fidelity gap in the corpus: a large part of the web is SVG.

The gap is in the engine, not in the HTML parsing: the elements reach the
document, they are simply never painted.

### `line-height` is ignored

`line-height: 1`, `1.5` and `2` produce exactly the same spacing, so the
property is not applied at all. The engine uses a fixed line box, and that
box is taller than the one Firefox computes: the whole page ends up with a
looser vertical rhythm than the reference, which is what the `diff` column
picks up on the text fixtures. Line height also decides where the next
line starts, so this shifts everything below the first paragraph.

### These properties are ignored

| Property | What happens instead |
| --- | --- |
| `text-indent` | the paragraph starts at the margin, not indented |
| `text-align: justify` | the text is left aligned, the spaces are not stretched |
| `text-transform: uppercase` | the text keeps the case it has in the markup |
| `font-variant-ligatures: none` | the ligature is applied anyway |

The first three are text layout features that Stylo computes but the
engine does not use. The fourth is different: the font features are
applied by the shaper, and the switch that would turn them off is not
reaching it.

### `@font-face` with a file url

The `@font-face` rule with `src: url("file:///...")` is not applied: the
text falls back to the default family. The reference draws the same line
with the extra bold file, so the difference is visible. The `local()`
form cannot be told apart on this machine, because it names the same file
the default family resolves to.

## Limits of this probe

- One family is installed on this machine, so nothing here says whether
  family *matching* (weights, styles, generic families) is right beyond
  what the weight fixture shows.
- Firefox is the reference, not the specification: where Firefox and the
  CSS specification disagree, the probe follows Firefox.
- The numbers are from one machine and one window size. The verdicts are
  what matter, not the decimals.
- Subresources are only covered by the SVG fixture: a missing image is
  indistinguishable from an image that failed to decode.
- The probe sees the window, so anything the engine draws outside the
  viewport is not measured.
