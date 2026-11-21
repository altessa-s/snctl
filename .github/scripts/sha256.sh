#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# SHA-256 sums and checks with whichever tool the platform has: macOS ships
# shasum, MSYS2 only coreutils' sha256sum. Both write and check the same
# "<hash>  <file>" lines, so arguments such as -c pass through unchanged.
set -euo pipefail
if command -v shasum > /dev/null; then
    exec shasum -a 256 "$@"
fi
exec sha256sum "$@"
