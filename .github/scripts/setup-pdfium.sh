#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Fetch the binding's pinned, checksum-verified PDFium SDK and write runner settings.
# Usage: setup-pdfium.sh [RPATH] (Linux); Windows chooses the MSYS2 architecture.
set -euo pipefail
case "$(uname -s)/$(uname -m)" in
    Linux/x86_64) platform=linux-x64 ;;
    Linux/aarch64 | Linux/arm64) platform=linux-arm64 ;;
    MINGW*/* | MSYS*/*)
        case "${MSYSTEM:-}" in
            CLANG64 | UCRT64) platform=win-x64 ;;
            CLANGARM64) platform=win-arm64 ;;
            *) echo 'unsupported MSYS2 environment' >&2; exit 1 ;;
        esac ;;
    *) echo 'PDFium setup is for Linux and Windows' >&2; exit 1 ;;
esac
module=$(go list -m -f '{{.Dir}}' github.com/altessa-s/snrender-go)
dir=${PDFIUM_DIR:-${RUNNER_TEMP:?}/pdfium}
if [[ "$platform" == win-* ]]; then
    module=$(cygpath -m "$module")
    dir=$(cygpath -m "$dir")
fi
PYTHONIOENCODING=utf-8 python3 "$module/internal/clib/snrender/scripts/fetch-pdfium.py" --platform "$platform" --output "$dir"
{
    echo "PDFIUM_DIR=$dir"
    # Go splits cgo flags itself: quote the whole argument, including -I/-L.
    # Runner/SDK paths may contain spaces, especially under Windows profiles.
    echo "CGO_CFLAGS=${CGO_CFLAGS:--O2 -g} \"-I$dir/include\""
    if [[ "$platform" == win-* ]]; then
        echo "CGO_LDFLAGS=${CGO_LDFLAGS:-} \"-L$dir/lib\""
        echo "PDFIUM_BIN=$(cygpath -u "$dir")/bin"
    else
        echo "CGO_LDFLAGS=${CGO_LDFLAGS:-} \"-L$dir/lib\" \"-Wl,-rpath,${1:-$dir/lib}\""
    fi
} >> "${GITHUB_ENV:?}"
