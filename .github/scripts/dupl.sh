#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.

# Scan production sources at rmctl's token threshold. Diagnostics fail too.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$root"
mkdir -p .cache
find cmd internal -type f -name '*.go' | grep -Ev '(_gen\.go|_string\.go|_test\.go|testdata/)' > .cache/dupl-files.txt
bin/tools/dupl -files -t 60 -plumbing < .cache/dupl-files.txt > .cache/dupl-output.txt 2> .cache/dupl-errors.txt
if [[ -s .cache/dupl-errors.txt ]]; then
    cat .cache/dupl-errors.txt >&2
    exit 2
fi
cat .cache/dupl-output.txt
if [[ ${1:-report} == check && -s .cache/dupl-output.txt ]]; then
    exit 1
fi
