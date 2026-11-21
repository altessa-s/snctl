#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Check a portable binary without constructing services backed by real secrets.
set -euo pipefail
bin=${1:?binary}
version=${2:?version}
run() {
    if [[ -n "${SMOKE_PATH:-}" ]]; then
        PATH="$SMOKE_PATH" "$bin" "$@"
    else
        "$bin" "$@"
    fi
}
[[ "$(run version)" == "${version#v}" ]]
run --version | grep -F "${version#v}"
run --help > /dev/null
run version --full --json | grep '"render": false' > /dev/null
run version --full --json | grep '"notes": true' > /dev/null
root=$(cd "$(dirname "$0")/../.." && pwd)
run text "$root/testdata/notebooks/blank.note" > /dev/null
echo 'Lite version, help, native notebook text and capability checks passed.'
