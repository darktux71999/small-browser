# css-spike: comparison harness

Measures the two style providers of the spike (`odd/tasks/css-spike.md`, S4) on
the same renderer and fixtures:

- **memory**: peak RSS of one render (`/usr/bin/time -v`), in KB;
- **fidelity**: ink share and 25%-fuzz pixel diff against a Firefox headless
  reference, inside the reference content box (same method as
  `docs/font-svg-fidelity.md`);
- **lines of code**: Rust glue added by each route plus the external C code it
  pulls in.

Everything is run from the repository root.

## Requirements

- Rust toolchain, Pango/cairo and `pkg-config` (the spike links them directly).
- The NetSurf libraries, built once (below).
- ImageMagick 7 (`magick`), Firefox (`firefox`), `python3`, GNU `time`.
  Without `/usr/bin/time` the harness falls back to sampling `/proc` VmHWM
  (`CSS_SPIKE_TIME=off` forces that path).
- No display is needed: Firefox runs with `--headless`.

## Build the NetSurf libraries

```sh
sh spikes/css/build-libcss.sh
```

It clones `libwapcaplet`, `libparserutils`, `libcss` and `netsurf-buildsystem`
into `~/.cache/css-spike/src` and installs static libraries into
`~/.cache/css-spike/prefix` (outside the worktree). Override the cache with
`CSS_SPIKE_CACHE=/path`. The harness reads the sources from the same cache to
count their lines; `--external-src DIR` points at another checkout.

## Run the spike directly

```sh
cargo run --quiet --manifest-path spikes/css/Cargo.toml -- \
    --all --provider cssrs --out-dir /tmp/opencode/css-spike-png
```

One PNG per fixture plus one `RESULT provider=... fixture=... nodes=... sheets=...
ops=... height=... png=...` line per page. Use `--fixture F --out PNG` (and
`--provider libcss` for the NetSurf route) to render a single page, and
`--width N` to change the viewport (default 800).

## Run the harness

```sh
sh spikes/css/compare.sh
```

It renders every fixture for both providers one render at a time, so the RSS is
per render, then writes and prints the tables. Real numbers need the release
path (the default `cargo run` would include Cargo's own memory in the RSS); the
harness measures whatever command it is given:

```sh
cargo build --manifest-path spikes/css/Cargo.toml
sh spikes/css/compare.sh --render-cmd "$PWD/spikes/css/target/debug/css-spike"
```

Options:

- `--providers cssrs` (or `libcss`, or both, which is the default) runs a
  subset.
- `--render-cmd '<prefix>'` replaces the renderer. The prefix receives
  `--fixture F --provider P --out PNG --width N`, so the harness can be
  exercised with a stub renderer that only writes placeholder PNGs.
- `--out-dir DIR` (default `/tmp/opencode/css-spike-results`),
  `--fixtures-dir DIR`, `--width N`, `--external-src DIR`.
- `--help` prints the same summary.

Results under `--out-dir`:

| File | Content |
| --- | --- |
| `results.md` | render table, LOC table and notes; also printed to stdout |
| `results.csv` | one row per provider × fixture |
| `loc.csv` | the lines-of-code summary |
| `png/` | provider PNGs, render stdout/stderr and `time -v` reports |
| `fidelity/` | Firefox reference PNGs, content-box crops, per-row JSON errors |

A provider that fails to render (for example `libcss` while its FFI is still
being implemented) does not abort the run: its rows are written with
`status=pending` and the failure in the note column. The same happens when
Firefox or the fidelity probe fails for a row. The script always finishes all
fixtures and exits 0 after writing the tables; configuration errors (unknown
provider, no fixtures) exit 2.

## Fidelity probe standalone

```sh
python3 spikes/css/fidelity.py ENGINE.png FIXTURE.html \
    --width 800 --height 1200 --out /tmp/opencode/fidelity
```

It screenshots `FIXTURE.html` with Firefox headless at the same viewport width,
finds the content box of the reference with `-trim` (12% fuzz), crops both
images to that box and prints one JSON object with `ink_share`,
`ink_share_ref` and `diff_pct` (plus the content-box geometry). `--reference
PNG` reuses a reference captured earlier; `--firefox PATH` overrides the binary
and `--timeout SECONDS` the 120 s limit. Firefox is killed by PID, never by a
command-line pattern.

## What the LOC table counts

- **cssrs**: `spikes/css/src/provider.rs` (shared glue) and `src/css.rs`.
- **libcss**: `spikes/css/src/provider.rs` plus the route files matching
  `spikes/css/src/*libcss*.rs` (currently `src/libcss_ffi.rs` when present),
  and every `*.c`/`*.h` under
  `~/.cache/css-spike/src/{libcss,libparserutils,libwapcaplet}`.
- `spikes/css/src/{style.rs,render.rs,ffi.rs}` are identical for both routes
  and are deliberately excluded from the per-route numbers.

`src/css.rs` is the engine's existing parser/cascade, so the cssrs route also
reuses that code indirectly; the table reports source size, not effort.
