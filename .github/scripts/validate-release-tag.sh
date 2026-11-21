#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# The checkout must have fetched tags before a manual release is accepted.
set -euo pipefail
tag=${1:?existing version tag}
if [[ ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
    echo 'invalid release tag' >&2
    exit 2
fi
if ! git rev-parse --verify "refs/tags/$tag^{commit}" > /dev/null; then
    echo "release tag does not exist: $tag" >&2
    exit 1
fi
