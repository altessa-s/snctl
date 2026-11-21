#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Write a Scoop manifest for a release to stdout.
#
# Usage: scoop-manifest.sh [--lite] VERSION CHECKSUMS
#
# VERSION is the release version without the leading "v"; CHECKSUMS is the
# release's checksums.txt. Without --lite it is snctl.json, the full build,
# whose Windows archive holds snctl.exe at its top with the DLLs it loads
# beside it; Scoop extracts the whole archive into the app directory and its
# shim runs snctl.exe there, so Windows finds them. With --lite it is
# snctl-lite.json, the build without the renderer: a single static snctl.exe.
# Both run as snctl; Scoop has no conflicts field, so installing both shims
# the same command and the later install wins.
#
# Both architectures require a checksum. Scoop's checkver and autoupdate
# fields can follow later releases independently of the bucket workflow.
set -euo pipefail

lite=0
if [[ "${1:-}" == --lite ]]; then
    lite=1
    shift
fi
version=${1:?version}
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo 'invalid version' >&2; exit 2; }
checksums=${2:?checksums file}
name=snctl
desc="Command-line client for the Supernote cloud"
if ((lite)); then
    name=snctl-lite
    desc="Command-line client for the Supernote cloud, without rendering"
fi
base="https://github.com/altessa-s/snctl/releases/download"

# checksum ARCH: the checksum of the Windows zip of ARCH, empty when absent.
checksum() {
    local file="${name}_${version}_windows_$1.zip"
    awk -v f="$file" '$2 == f || $2 == "*" f { print $1 }' "$checksums"
}

amd64=$(checksum amd64)
[[ "$amd64" =~ ^[0-9a-f]{64}$ ]] || { printf 'no checksum for %s_%s_windows_amd64.zip\n' "$name" "$version" >&2; exit 1; }
arm64=$(checksum arm64)
if [[ ! "$arm64" =~ ^[0-9a-f]{64}$ ]]; then
    printf 'no checksum for %s_%s_windows_arm64.zip\n' "$name" "$version" >&2
    exit 1
fi

# architectures: the "architecture" entries, with their hashes, and
# autoupdate: the same without.
architectures=$(
    cat <<EOF
        "64bit": {
            "url": "${base}/v${version}/${name}_${version}_windows_amd64.zip",
            "hash": "${amd64}"
        }
EOF
)
autoupdate=$(
    cat <<EOF
            "64bit": {
                "url": "${base}/v\$version/${name}_\$version_windows_amd64.zip"
            }
EOF
)
if [[ -n "$arm64" ]]; then
    architectures+=$(
        cat <<EOF
,
        "arm64": {
            "url": "${base}/v${version}/${name}_${version}_windows_arm64.zip",
            "hash": "${arm64}"
        }
EOF
    )
    autoupdate+=$(
        cat <<EOF
,
            "arm64": {
                "url": "${base}/v\$version/${name}_\$version_windows_arm64.zip"
            }
EOF
    )
fi

cat <<EOF
{
    "version": "${version}",
    "description": "${desc}",
    "homepage": "https://github.com/altessa-s/snctl",
    "license": "MIT",
    "architecture": {
${architectures}
    },
    "bin": "snctl.exe",
    "checkver": "github",
    "autoupdate": {
        "architecture": {
${autoupdate}
        },
        "hash": {
            "url": "\$baseurl/checksums.txt"
        }
    }
}
EOF
