#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Use MSYS2's native C23 compiler and pkg-config for the installed Cairo stack.
set -euo pipefail
case "${MSYSTEM:-}" in
    CLANG64 | CLANGARM64 | UCRT64) ;;
    *) echo 'run in MSYS2 CLANG64, CLANGARM64 or UCRT64' >&2; exit 1 ;;
esac
compiler=clang
[[ "$MSYSTEM" != UCRT64 ]] || compiler=gcc
for tool in "$compiler" pkg-config python3 go; do
    command -v "$tool" > /dev/null
done
{
    echo "CC=$compiler"
    echo "CGO_ENABLED=1"
} >> "${GITHUB_ENV:?}"

bash "$(dirname "$0")/setup-pdfium.sh"
