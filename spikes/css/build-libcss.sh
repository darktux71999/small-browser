#!/bin/sh
# Build NetSurf's libwapcaplet + libparserutils + libcss (all MIT) into a scratch
# prefix outside the worktree, for the css-spike comparison (odd/tasks/css-spike.md, S6/T1).
#
# Usage: sh spikes/css/build-libcss.sh
# Result: ~/.cache/css-spike/prefix/{include,lib} + a link smoke test.
set -eu

CACHE=${CSS_SPIKE_CACHE:-"$HOME/.cache/css-spike"}
SRC="$CACHE/src"
PREFIX="$CACHE/prefix"

mkdir -p "$SRC" "$PREFIX"

clone() {
    [ -d "$SRC/$1" ] && return 0
    echo "== clone $1"
    git clone --depth 1 -q "https://github.com/netsurf-browser/$1" "$SRC/$1"
}

clone buildsystem
clone libwapcaplet
clone libparserutils
clone libcss

NSSHARED="$SRC/buildsystem"

# musl sin iconv: libparserutils sabe prescindir del filtro iconv.
if ! printf '#include <iconv.h>\nint main(void){ iconv_open("UTF-8","UTF-8"); return 0; }\n' \
        | ${CC:-cc} -x c - -o "$CACHE/iconv-smoke" 2>/dev/null; then
    echo "== sin iconv: libparserutils se construye con WITHOUT_ICONV_FILTER"
    grep -q WITHOUT_ICONV_FILTER "$SRC/libparserutils/Makefile.config" 2>/dev/null \
        || echo "WITHOUT_ICONV_FILTER := yes" >> "$SRC/libparserutils/Makefile.config"
fi

# C23 rompe este código antiguo y el buildsystem impone -Werror: se fijan por línea
# de comandos (sobrescriben las asignaciones del Makefile).
CC_SMOKE="${CC:-cc}"

# El buildsystem llama a `install -C`; busybox install no conoce -C. Shim que lo elimina.
REAL_INSTALL=$(command -v install)
mkdir -p "$CACHE/bin"
cat > "$CACHE/bin/install" <<EOF
#!/bin/sh
args=
for a in "\$@"; do
    [ "\$a" = "-C" ] && continue
    args="\$args \$a"
done
exec $REAL_INSTALL \$args
EOF
chmod +x "$CACHE/bin/install"

build() {
    name=$1
    echo "== build $name"
    PATH="$CACHE/bin:$PATH" make -C "$SRC/$name" NSSHARED="$NSSHARED" PREFIX="$PREFIX" \
        COMPONENT_TYPE=lib-static WARNFLAGS="-Wall -Wno-error" \
        CC="$CC_SMOKE -std=gnu17" install
}

build libwapcaplet
build libparserutils
build libcss

echo "== smoke test"
cat > "$CACHE/smoke.c" <<'EOF'
#include <libcss/libcss.h>
#include <libwapcaplet/libwapcaplet.h>
#include <stdio.h>
#include <string.h>

static css_error resolve(void *pw, const char *base, lwc_string *rel, lwc_string **abs)
{
    (void)pw; (void)base;
    *abs = lwc_string_ref(rel);
    return CSS_OK;
}

int main(void)
{
    lwc_string *s = NULL;
    if (lwc_intern_string("div", 3, &s) != lwc_error_ok) return 1;

    css_stylesheet_params params;
    memset(&params, 0, sizeof params);
    params.params_version = CSS_STYLESHEET_PARAMS_VERSION_1;
    params.level = CSS_LEVEL_DEFAULT;
    params.title = "smoke";
    params.url = "about:smoke";
    params.resolve = resolve;

    css_stylesheet *sheet = NULL;
    if (css_stylesheet_create(&params, &sheet) != CSS_OK) return 2;
    css_stylesheet_destroy(sheet);
    lwc_string_unref(s);
    puts("libcss smoke: OK");
    return 0;
}
EOF
$CC_SMOKE -std=gnu17 "$CACHE/smoke.c" -I"$PREFIX/include" \
    "$PREFIX/lib/libcss.a" "$PREFIX/lib/libparserutils.a" "$PREFIX/lib/libwapcaplet.a" \
    -o "$CACHE/smoke"
"$CACHE/smoke"
echo "prefix: $PREFIX"
