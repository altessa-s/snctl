#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Install the Cairo stack and C23 compiler required by snrender-go v1.0.0.
set -euo pipefail
sudo=""
if [[ "$(id -u)" != 0 ]]; then
    sudo=sudo
fi
$sudo apt-get update -q
$sudo apt-get install -y -q --no-install-recommends clang-19 pkg-config python3 \
    libcairo2-dev libfreetype-dev libgdk-pixbuf-2.0-dev zlib1g-dev
{
    echo "CC=clang-19"
    echo "CGO_ENABLED=1"
} >> "${GITHUB_ENV:?}"

bash "$(dirname "$0")/setup-pdfium.sh" "${1:-}"
