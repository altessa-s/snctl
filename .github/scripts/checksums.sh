#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Hash only release archives, with paths relative to the release directory.
set -euo pipefail
sha256="$(cd "$(dirname "$0")" && pwd)/sha256.sh"
cd "${1:?release directory}"
shopt -s nullglob
archives=(snctl_*.zip snctl_*.tar.gz snctl-lite_*.zip snctl-lite_*.tar.gz)
((${#archives[@]} > 0))
bash "$sha256" "${archives[@]}" > checksums.txt
cat checksums.txt
