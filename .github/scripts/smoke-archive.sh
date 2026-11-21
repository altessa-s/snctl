#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Exercise the archive from a clean extraction, then run the file workflow
# against the loopback cloud with synthetic file secrets and no login.
set -euo pipefail
sha256="$(cd "$(dirname "$0")" && pwd)/sha256.sh"
archive=${1:?archive}
version=${2:?version}
root=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$root/bin/.tmp"
work=$(mktemp -d "$root/bin/.tmp/archive.XXXXXX")
trap 'rm -rf "$work"' EXIT
case "$archive" in
    *.zip) unzip -q "$archive" -d "$work/app" ;;
    *.tar.gz) mkdir -p "$work/app"; tar -xzf "$archive" -C "$work/app" ;;
    *) exit 2 ;;
esac
bin="$work/app/snctl"
test -d "$work/app/licenses/go"
(cd "$work/app" && bash "$sha256" -c licenses.sha256 > /dev/null)
[[ ! -f "$bin.exe" ]] || bin="$bin.exe"
export SNCTL_CONFIG_DIR="$work/config" SNCTL_CONFIG_FILE="$work/config/config.toml"
export SNCTL_CACHE_DIR="$work/cache" SNCTL_PROFILE=default SNCTL_SECRETS=file
if [[ "$(uname -s)" == MINGW* || "$(uname -s)" == MSYS* ]]; then
    system=$(cygpath -u "${SYSTEMROOT:-C:/Windows}")
    export SMOKE_PATH="$system/System32:$system"
    if [[ "$archive" == *snctl-lite_* ]]; then
        bash "$root/.github/scripts/check-windows-static.sh" "$bin"
    else
        bash "$root/.github/scripts/bundle-windows-dlls.sh" --verify "$bin"
    fi
fi
run() {
    if [[ -n "${SMOKE_PATH:-}" ]]; then
        PATH="$SMOKE_PATH" "$bin" "$@"
    else
        env -u LD_LIBRARY_PATH -u LD_PRELOAD "$bin" "$@"
    fi
}
if [[ "$archive" == *snctl-lite_* ]]; then
    bash "$root/.github/scripts/smoke-lite.sh" "$bin" "$version"
else
    [[ "$(run version)" == "${version#v}" ]]
    run --version | grep -F "${version#v}"
    run --help > /dev/null
    run version --full --json | grep '"render": true' > /dev/null
    case "$(uname -s)" in
        Linux)
            library="$work/app/libpdfium.so"
            readelf -d "$bin" | grep -E 'R(UN)?PATH' | grep -qF "[\$ORIGIN]"
            resolved=$(env -u LD_LIBRARY_PATH -u LD_PRELOAD ldd "$bin" | awk '/libpdfium.so =>/ {print $3}')
            [[ "$resolved" == "$library" ]] || { echo "PDFium loaded outside archive: $resolved" >&2; exit 1; }
            ;;
        MINGW* | MSYS*) library="$work/app/pdfium.dll" ;;
        *) library='' ;;
    esac
    if [[ -n "$library" ]]; then
        test -s "$library"
        mv "$library" "$library.hidden"
        if run version > /dev/null 2>&1; then
            echo 'archive runs without its PDFium; a system/build-tree copy was used' >&2
            exit 1
        fi
        mv "$library.hidden" "$library"
    fi
    run render "$root/testdata/overlay/Report.pdf.mark" -o "$work/overlay.pdf"
    python3 - "$root/testdata/overlay/Report.pdf" "$work/overlay.pdf" <<'PYTHON'
from pathlib import Path
import sys
source, output = map(Path, sys.argv[1:])
assert output.read_bytes().startswith(source.read_bytes()), 'source PDF was not preserved by overlay'
PYTHON
    # A PNG preview actually calls PDFium on Cairo (PDF export alone does not).
    run render "$root/testdata/overlay/Report.pdf.mark" --page 1 --scale .1 -o "$work/overlay.png"
    python3 - "$work/overlay.png" <<'PYTHON'
from pathlib import Path
import sys
assert Path(sys.argv[1]).read_bytes().startswith(b'\x89PNG\r\n\x1a\n'), 'overlay preview is not PNG'
PYTHON
    echo 'Source PDF overlay and bundled runtime smoke passed.'
    sample="$root/testdata/notebooks/blank.note"
    run render "$sample" -o "$work/smoke.pdf"
    head -c 5 "$work/smoke.pdf" | grep -q '%PDF-'
    echo 'Full notebook PDF smoke passed.'
fi
# Build the harness without native dependencies; it invokes the extracted binary.
if command -v cygpath > /dev/null; then
    bin=$(cygpath -m "$bin")
fi
CGO_ENABLED=0 SNCTL_TEST_BINARY="$bin" go test -count=1 -run '^TestE2EFileCommands$' ./cmd/snctl
