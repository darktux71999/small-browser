#!/bin/sh
# css-spike measurement harness (odd/tasks/css-spike.md, T4/S4).
#
# Renders every fixture with each style provider, measures per-render peak RSS
# and wall time, compares the PNG against a Firefox headless reference (ink
# share + 25%-fuzz pixel diff inside the reference content box), counts the
# lines of code each route adds, and writes a markdown + CSV table.
#
# Usage:
#   sh spikes/css/compare.sh [options]
#
# Options:
#   --providers LIST      space- or comma-separated: cssrs, libcss (default both)
#   --render-cmd CMD      renderer prefix, receives
#                         --fixture F --provider P --out PNG --width N
#                         (default: cargo run --quiet --manifest-path <crate>/Cargo.toml --)
#   --out-dir DIR         output and results directory
#                         (default: /tmp/opencode/css-spike-results)
#   --fixtures-dir DIR    fixtures directory (default: <crate>/fixtures)
#   --width N             viewport width (default 800)
#   --external-src DIR    NetSurf sources for the LOC count
#                         (default: ~/.cache/css-spike/src)
#   -h, --help            this help
#
# A failing provider never aborts the run: its rows are written as `pending`
# with the reason in the note column, so the libcss route can fail while the
# other worker lands it.
set -u

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
CRATE=$SCRIPT_DIR
REPO=$(CDPATH= cd -- "$CRATE/../.." && pwd)

OUT_DIR=/tmp/opencode/css-spike-results
FIXTURES=$CRATE/fixtures
PROVIDERS="cssrs libcss"
RENDER_CMD=
WIDTH=800
EXTERNAL_SRC=${CSS_SPIKE_SRC:-"$HOME/.cache/css-spike/src"}

usage() {
    code=${1:-0}
    [ $# -ge 1 ] && shift
    [ $# -ge 1 ] && printf 'compare.sh: %s\n\n' "$1" >&2
    cat <<'EOF'
css-spike comparison harness (S4)

Usage: sh spikes/css/compare.sh [options]

  --providers LIST      space- or comma-separated: cssrs, libcss (default both)
  --render-cmd CMD      renderer prefix; receives --fixture F --provider P
                        --out PNG --width N
                        (default: cargo run --quiet --manifest-path <crate>/Cargo.toml --)
  --out-dir DIR         results directory (default: /tmp/opencode/css-spike-results)
  --fixtures-dir DIR    fixtures directory (default: <crate>/fixtures)
  --width N             viewport width (default 800)
  --external-src DIR    NetSurf sources for the LOC count
                        (default: ~/.cache/css-spike/src)
  -h, --help            this help

Outputs (under --out-dir): results.md, results.csv, loc.csv, png/, fidelity/.
EOF
    exit "$code"
}

while [ $# -gt 0 ]; do
    case "$1" in
        --providers)
            [ $# -ge 2 ] || usage 2 "missing value for --providers"
            PROVIDERS=$(printf '%s' "$2" | tr ',' ' ')
            shift 2
            ;;
        --render-cmd)
            [ $# -ge 2 ] || usage 2 "missing value for --render-cmd"
            RENDER_CMD=$2
            shift 2
            ;;
        --out-dir)
            [ $# -ge 2 ] || usage 2 "missing value for --out-dir"
            OUT_DIR=$2
            shift 2
            ;;
        --fixtures-dir)
            [ $# -ge 2 ] || usage 2 "missing value for --fixtures-dir"
            FIXTURES=$2
            shift 2
            ;;
        --width)
            [ $# -ge 2 ] || usage 2 "missing value for --width"
            WIDTH=$2
            shift 2
            ;;
        --external-src)
            [ $# -ge 2 ] || usage 2 "missing value for --external-src"
            EXTERNAL_SRC=$2
            shift 2
            ;;
        -h|--help)
            usage 0
            ;;
        *)
            usage 2 "unknown option: $1"
            ;;
    esac
done

case "$WIDTH" in
    ''|*[!0-9]*) usage 2 "--width must be a positive integer" ;;
esac
[ "$WIDTH" -gt 0 ] || usage 2 "--width must be a positive integer"

PROVIDERS=$(printf '%s' "$PROVIDERS" | tr -s ' ' | sed 's/^ //; s/ $//')
[ -n "$PROVIDERS" ] || usage 2 "--providers is empty"
for provider in $PROVIDERS; do
    case "$provider" in
        cssrs|libcss) ;;
        *) usage 2 "unknown provider: $provider (expected cssrs or libcss)" ;;
    esac
done

[ -d "$FIXTURES" ] || usage 2 "fixtures directory not found: $FIXTURES"
fixture_count=0
for fixture in "$FIXTURES"/*.html; do
    [ -f "$fixture" ] && fixture_count=$((fixture_count + 1))
done
[ "$fixture_count" -gt 0 ] || usage 2 "no *.html fixtures in $FIXTURES"

if [ -z "$RENDER_CMD" ]; then
    RENDER_CMD="cargo run --quiet --manifest-path '$CRATE/Cargo.toml' --"
fi

mkdir -p "$OUT_DIR/png" "$OUT_DIR/fidelity" || {
    printf 'compare.sh: cannot create %s\n' "$OUT_DIR" >&2
    exit 2
}

PNG_DIR=$OUT_DIR/png
FID_DIR=$OUT_DIR/fidelity
ROWS=$OUT_DIR/rows.tsv
CAPTURED=$OUT_DIR/captured-refs.txt
: > "$ROWS"
: > "$CAPTURED"

# GNU time is the reliable way to get the peak RSS of a child.
# `CSS_SPIKE_TIME=off` forces the /proc VmHWM sampler; a path overrides the
# binary.
TIME_BIN=
case ${CSS_SPIKE_TIME:-auto} in
    off) ;;
    auto) [ -x /usr/bin/time ] && TIME_BIN=/usr/bin/time ;;
    *) [ -x "$CSS_SPIKE_TIME" ] && TIME_BIN=$CSS_SPIKE_TIME ;;
esac

# --- LOC -------------------------------------------------------------------
# Count the Rust glue each route adds and the external C it pulls in. The
# shared style/render/ffi files are identical for both routes and stay out of
# the per-route numbers; provider.rs is shared glue and is counted for both.
line_count() {
    total=0
    for file in "$@"; do
        [ -f "$file" ] || continue
        total=$((total + $(wc -l < "$file")))
    done
    printf '%s' "$total"
}

external_loc() {
    total=0
    for dir in "$@"; do
        [ -d "$dir" ] || continue
        count=$(find "$dir" -type f \( -name '*.c' -o -name '*.h' \) -exec cat {} + 2>/dev/null | wc -l)
        total=$((total + count))
    done
    printf '%s' "$total"
}

PROVIDER_RS=$CRATE/src/provider.rs
LIBCSS_FFI_FILES=
for file in "$CRATE"/src/*libcss*.rs; do
    [ -f "$file" ] && LIBCSS_FFI_FILES="$LIBCSS_FFI_FILES $file"
done
LIBCSS_FFI_FILES=${LIBCSS_FFI_FILES# }

CSSRS_GLUE=$(line_count "$PROVIDER_RS")
CSSRS_EXTERNAL=$(line_count "$REPO/src/css.rs")
LIBCSS_GLUE=$(line_count "$PROVIDER_RS" $LIBCSS_FFI_FILES)
LIBCSS_EXTERNAL=$(external_loc \
    "$EXTERNAL_SRC/libcss" "$EXTERNAL_SRC/libparserutils" "$EXTERNAL_SRC/libwapcaplet")

# --- render ----------------------------------------------------------------
# $RENDER_CMD is expanded as a shell fragment (same contract as eval) and the
# appended options are passed as positional parameters, so quotes in the
# command survive while the fixture path does not need re-quoting.
render_once() {
    # $1 = provider, $2 = fixture, $3 = png, $4 = stdout file, $5 = stderr file
    provider=$1
    fixture=$2
    png=$3
    stdout_file=$4
    stderr_file=$5

    if [ -n "$TIME_BIN" ]; then
        time_file=$PNG_DIR/$(basename "$fixture" .html)-$provider.time
        "$TIME_BIN" -v -o "$time_file" sh -c "$RENDER_CMD \"\$@\"" sh \
            --fixture "$fixture" --provider "$provider" --out "$png" --width "$WIDTH" \
            > "$stdout_file" 2> "$stderr_file"
        render_rc=$?
        RSS_KB=-
        WALL_S=-
        if [ -f "$time_file" ]; then
            RSS_KB=$(awk -F': ' '/Maximum resident set size/{gsub(/[^0-9]/,"",$2); print $2}' "$time_file")
            wall_raw=$(awk -F': ' '/Elapsed \(wall clock\)/{gsub(/^[[:space:]]+/,"",$2); print $2}' "$time_file")
            WALL_S=$(awk -v t="$wall_raw" 'BEGIN{
                if (index(t, ":") > 0) {
                    n = split(t, a, ":")
                    if (n == 3) s = a[1]*3600 + a[2]*60 + a[3]
                    else if (n == 2) s = a[1]*60 + a[2]
                    else s = a[1] + 0
                } else {
                    # `time -v` variants print "0m 0.38s" or "1h 2m 3s".
                    s = 0
                    n = split(t, a, /[[:space:]]+/)
                    for (i = 1; i <= n; i++) {
                        v = a[i] + 0
                        if (a[i] ~ /h/) s += v * 3600
                        else if (a[i] ~ /m/) s += v * 60
                        else if (a[i] ~ /s/) s += v
                    }
                }
                printf "%.3f", s
            }')
        fi
        [ -n "$RSS_KB" ] || RSS_KB=-
        [ -n "$WALL_S" ] || WALL_S=-
    else
        # Fallback: sample VmHWM from /proc while the render runs.
        start_ns=$(date +%s%N)
        sh -c "$RENDER_CMD \"\$@\"" sh \
            --fixture "$fixture" --provider "$provider" --out "$png" --width "$WIDTH" \
            > "$stdout_file" 2> "$stderr_file" &
        render_pid=$!
        RSS_KB=0
        while [ -r "/proc/$render_pid/status" ]; do
            render_state=$(awk '/^State:/{print $2}' "/proc/$render_pid/status" 2>/dev/null)
            [ "$render_state" = "Z" ] && break
            sample=$(awk '/^VmHWM:/{print $2}' "/proc/$render_pid/status" 2>/dev/null)
            if [ -n "$sample" ] && [ "$sample" -gt "$RSS_KB" ]; then
                RSS_KB=$sample
            fi
            sleep 0.02
        done
        wait "$render_pid"
        render_rc=$?
        end_ns=$(date +%s%N)
        WALL_S=$(awk -v a="$start_ns" -v b="$end_ns" 'BEGIN{printf "%.3f", (b - a) / 1000000000}')
        [ "$RSS_KB" -gt 0 ] || RSS_KB=-
    fi
}

result_field() {
    # $1 = RESULT line, $2 = key
    printf '%s\n' "$1" | tr ' ' '\n' | sed -n "s/^$2=//p" | head -n 1
}

fidelity_field() {
    # $1 = JSON, $2 = key; prints "-" for null/missing
    printf '%s' "$1" | python3 -c '
import json, sys
try:
    value = json.load(sys.stdin).get(sys.argv[1])
except Exception:
    value = None
print("-" if value is None else value)
' "$2" 2>/dev/null
}

# --- run -------------------------------------------------------------------
printf 'css-spike comparison: providers=%s width=%s\n' "$PROVIDERS" "$WIDTH" >&2
printf 'render command: %s\n' "$RENDER_CMD" >&2

pending_rows=0
for provider in $PROVIDERS; do
    for fixture in "$FIXTURES"/*.html; do
        [ -f "$fixture" ] || continue
        stem=$(basename "$fixture" .html)
        png=$PNG_DIR/$stem-$provider.png
        stdout_file=$PNG_DIR/$stem-$provider.out
        stderr_file=$PNG_DIR/$stem-$provider.err
        printf '== %s %s\n' "$provider" "$stem" >&2

        : > "$stdout_file"
        : > "$stderr_file"
        render_once "$provider" "$fixture" "$png" "$stdout_file" "$stderr_file"

        result_line=$(grep '^RESULT ' "$stdout_file" 2>/dev/null | tail -n 1)
        nodes=$(result_field "$result_line" nodes)
        sheets=$(result_field "$result_line" sheets)
        ops=$(result_field "$result_line" ops)
        height=$(result_field "$result_line" height)
        status=ok
        note=

        if [ "$render_rc" -ne 0 ] || [ -z "$result_line" ] || [ ! -f "$png" ]; then
            status=pending
            detail=$(head -n 1 "$stderr_file" 2>/dev/null)
            [ -n "$detail" ] || detail="render exited $render_rc without a RESULT line or PNG"
            note="render: $detail"
            [ -n "$nodes" ] || nodes=-
            [ -n "$sheets" ] || sheets=-
            [ -n "$ops" ] || ops=-
            [ -n "$height" ] || height=-
            pending_rows=$((pending_rows + 1))
        fi

        ink=-
        ink_ref=-
        diff=-
        if [ "$status" = ok ]; then
            ref_height=$height
            case "$ref_height" in
                ''|*[!0-9]*) ref_height=1200 ;;
            esac
            [ "$ref_height" -lt 1200 ] && ref_height=1200

            if grep -Fqx "$stem" "$CAPTURED" 2>/dev/null; then
                fid_json=$(python3 "$SCRIPT_DIR/fidelity.py" "$png" "$fixture" \
                    --width "$WIDTH" --height "$ref_height" --out "$FID_DIR" \
                    --reference "$FID_DIR/$stem-ref.png" 2>"$PNG_DIR/$stem-$provider.fidelity.err")
            else
                fid_json=$(python3 "$SCRIPT_DIR/fidelity.py" "$png" "$fixture" \
                    --width "$WIDTH" --height "$ref_height" --out "$FID_DIR" \
                    2>"$PNG_DIR/$stem-$provider.fidelity.err")
            fi
            fid_rc=$?
            if [ "$fid_rc" -eq 0 ]; then
                ink=$(fidelity_field "$fid_json" ink_share)
                ink_ref=$(fidelity_field "$fid_json" ink_share_ref)
                diff=$(fidelity_field "$fid_json" diff_pct)
                [ -n "$ink" ] || ink=-
                [ -n "$ink_ref" ] || ink_ref=-
                [ -n "$diff" ] || diff=-
                grep -Fqx "$stem" "$CAPTURED" 2>/dev/null || printf '%s\n' "$stem" >> "$CAPTURED"
            else
                status=pending
                detail=$(fidelity_field "$fid_json" error)
                if [ "$detail" = "-" ] || [ -z "$detail" ]; then
                    detail=$(head -n 1 "$PNG_DIR/$stem-$provider.fidelity.err" 2>/dev/null)
                fi
                [ -n "$detail" ] || detail="fidelity probe failed"
                note="fidelity: $detail"
                ink=-
                ink_ref=-
                diff=-
                pending_rows=$((pending_rows + 1))
            fi
        fi

        clean_note=$(printf '%s' "$note" | tr '\t\r\n' '   ' | cut -c1-200)
        printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
            "$provider" "$stem" "$status" "$RSS_KB" "$WALL_S" \
            "$ink" "$ink_ref" "$diff" \
            "$nodes" "$sheets" "$ops" "$height" "$png" "$clean_note" >> "$ROWS"

        printf '   %s rss=%sKB wall=%ss ink=%s/%s diff=%s%%\n' \
            "$status" "$RSS_KB" "$WALL_S" "$ink" "$ink_ref" "$diff" >&2
    done
done

# --- tables ----------------------------------------------------------------
LOC_CSSRS_TOTAL=$((CSSRS_GLUE + CSSRS_EXTERNAL))
LOC_LIBCSS_TOTAL=$((LIBCSS_GLUE + LIBCSS_EXTERNAL))

{
    printf '# css-spike comparison\n\n'
    printf '%s\n' "- Generated: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf '%s\n' "- Providers: $PROVIDERS"
    printf '%s\n' "- Viewport width: $WIDTH px"
    printf '%s\n' "- Render command: \`$RENDER_CMD\`"
    printf '%s\n' "- Fidelity: ink share and pixel diff (25% fuzz) inside the Firefox reference content box"
    printf '\n## Render results\n\n'
    awk -F'\t' 'BEGIN {
        print "| Provider | Fixture | Status | RSS (KB) | Wall (s) | Ink | Ink ref | Diff % | Nodes | Sheets | Ops | Height | PNG |"
        print "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |"
    }
    {
        for (i = 1; i <= NF; i++) gsub(/\|/, "/", $i)
        printf "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", \
            $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
    }' "$ROWS"

    printf '\n## Lines of code\n\n'
    printf '| Route | Rust glue (LOC) | External code (LOC) | Total (LOC) |\n'
    printf '| --- | --- | --- | --- |\n'
    printf '| cssrs | %s | %s | %s |\n' "$CSSRS_GLUE" "$CSSRS_EXTERNAL" "$LOC_CSSRS_TOTAL"
    printf '| libcss | %s | %s | %s |\n' "$LIBCSS_GLUE" "$LIBCSS_EXTERNAL" "$LOC_LIBCSS_TOTAL"

    printf '\nCounted files:\n\n'
    printf '%s\n' "- shared glue (both routes): \`spikes/css/src/provider.rs\` ($(line_count "$PROVIDER_RS") lines)"
    if [ -n "$LIBCSS_FFI_FILES" ]; then
        printf '%s\n' "- libcss FFI: $LIBCSS_FFI_FILES ($(line_count $LIBCSS_FFI_FILES) lines)"
    else
        printf '%s\n' "- libcss FFI: none found (\`spikes/css/src/libcss_ffi.rs\` absent, counted as 0)"
    fi
    printf '%s\n' "- cssrs external code: \`src/css.rs\` ($(line_count "$REPO/src/css.rs") lines)"
    printf '%s\n' "- libcss external code: \`*.c\` + \`*.h\` under \`$EXTERNAL_SRC/{libcss,libparserutils,libwapcaplet}\` ($LIBCSS_EXTERNAL lines)"
    printf '%s\n' "- not counted per route (identical for both): \`spikes/css/src/style.rs\`, \`spikes/css/src/render.rs\`, \`spikes/css/src/ffi.rs\`"

    printf '\n## Notes\n\n'
    if awk -F'\t' '$3 != "ok" && $14 != "" {found = 1} END {exit !found}' "$ROWS"; then
        awk -F'\t' '$3 != "ok" && $14 != "" {printf "- `%s / %s`: %s\n", $1, $2, $14}' "$ROWS"
    else
        printf '%s\n' "- No pending rows: every render and fidelity probe succeeded."
    fi
    printf '\nArtifacts: PNGs in `png/`, Firefox references and crops in `fidelity/`, raw rows in `rows.tsv`.\n'
} > "$OUT_DIR/results.md"

{
    printf 'provider,fixture,status,max_rss_kb,wall_s,ink_share,ink_share_ref,diff_pct,nodes,sheets,ops,height,png,note\n'
    awk -F'\t' 'BEGIN {OFS = ","}
    {
        for (i = 1; i <= NF; i++) gsub(/[",]/, ";", $i)
        print
    }' "$ROWS"
} > "$OUT_DIR/results.csv"

{
    printf 'route,glue_rust_loc,external_loc,total_loc\n'
    printf 'cssrs,%s,%s,%s\n' "$CSSRS_GLUE" "$CSSRS_EXTERNAL" "$LOC_CSSRS_TOTAL"
    printf 'libcss,%s,%s,%s\n' "$LIBCSS_GLUE" "$LIBCSS_EXTERNAL" "$LOC_LIBCSS_TOTAL"
} > "$OUT_DIR/loc.csv"

printf '\n'
cat "$OUT_DIR/results.md"
if [ "$pending_rows" -gt 0 ]; then
    printf '\nWARNING: %s pending row(s); see the notes in %s\n' "$pending_rows" "$OUT_DIR/results.md" >&2
fi
printf 'Wrote %s, %s and %s\n' "$OUT_DIR/results.md" "$OUT_DIR/results.csv" "$OUT_DIR/loc.csv" >&2
exit 0
