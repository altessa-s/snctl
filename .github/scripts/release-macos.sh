#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Build, sign, notarize and upload macOS archives of an already published tag.
# Normal runs fetch the tag from origin and build in a temporary detached worktree;
# the caller's checkout may be on any branch with tracked or untracked changes.
# --dry-run builds the current checkout offline (cached Go dependencies required),
# without fetching a tag, creating a worktree, accessing credentials or using GH.
set -euo pipefail
dry=0
if [[ "${1:-}" == --dry-run ]]; then
    dry=1
    shift
fi
tag=${1:?tag, such as v1.0.0}
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { printf 'not a release tag: %s\n' "$tag" >&2; exit 2; }
[[ $# == 1 ]] || { echo 'usage: release-macos.sh [--dry-run] TAG' >&2; exit 2; }
[[ "$(uname -s)" == Darwin ]] || { echo 'run this on a Mac' >&2; exit 1; }
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
repo=altessa-s/snctl
identity=${MACOS_SIGN_IDENTITY:-Developer ID Application: Altessa Solutions Inc (SZA4GU6AL2)}
profile=${NOTARY_PROFILE:-rmctl-notary}
archs=${RELEASE_ARCHS:-arm64 amd64}
for arch in $archs; do
    case "$arch" in arm64 | amd64) ;; *) printf 'unsupported macOS architecture: %s\n' "$arch" >&2; exit 2 ;; esac
done
work=''
tree=''
source=$root
cleanup() {
    if [[ -n "$tree" ]]; then
        git -C "$root" worktree remove --force "$tree" 2>/dev/null || true
    fi
    [[ -z "$work" ]] || rm -rf "$work"
}
trap cleanup EXIT
if ((!dry)); then
    [[ "$archs" == 'arm64 amd64' ]] || { echo 'uploads require RELEASE_ARCHS="arm64 amd64" (use --dry-run for one architecture)' >&2; exit 1; }
    release=$(gh release view "$tag" --repo "$repo" --json isDraft,isPrerelease) || { printf 'no release %s yet: wait for its release workflow\n' "$tag" >&2; exit 1; }
    [[ "$(jq -r .isDraft <<<"$release")" == false ]] || { echo 'release is still a draft; wait for its release workflow' >&2; exit 1; }
    prerelease=$(jq -r .isPrerelease <<<"$release")
    security find-identity -v -p codesigning | grep -F "$identity" > /dev/null || { printf 'no signing identity %s\n' "$identity" >&2; exit 1; }
    work=$(mktemp -d)
    git -C "$root" fetch --quiet origin "refs/tags/$tag:refs/tags/$tag" || { printf 'could not fetch tag %s from origin\n' "$tag" >&2; exit 1; }
    tree="$work/src"
    git -C "$root" worktree add --quiet --detach "$tree" "refs/tags/$tag"
    source=$tree
else
    # Prevent module, checksum database, VCS and toolchain downloads, including
    # direct VCS downloads that bypass GOPROXY.
    export GOPROXY=off GOSUMDB=off GOVCS='*:off' GOTOOLCHAIN=local
fi
export RELEASE_DIR=${RELEASE_DIR:-$root/bin/release}
mkdir -p "$RELEASE_DIR"
RELEASE_DIR=$(cd "$RELEASE_DIR" && pwd)
export RELEASE_DIR
# Real runs always stage afresh, including when retrying a partial upload.
dest=$RELEASE_DIR
if ((!dry)); then
    dest="$work/build"
fi
cd "$source"
for arch in $archs; do
    cc='clang -arch arm64'
    [[ "$arch" != amd64 ]] || cc='clang -arch x86_64'
    for variant in full lite; do
        GOOS=darwin GOARCH="$arch" CC="$cc" RELEASE_DIR="$dest" RELEASE_STAGE_ONLY=1 bash "$source/.github/scripts/release-build.sh" "$tag" "$variant"
    done
done
if ((dry)); then
    echo 'Signing, notarization and publication skipped: dry run.'
else
    mkdir -p "$work/notarize"
    for arch in $archs; do
        for name in snctl snctl-lite; do
            bin="$dest/stage/$name-darwin-$arch/snctl"
            codesign --force --options runtime --timestamp --identifier io.altessa.snctl --sign "$identity" "$bin"
            codesign --verify --strict --verbose=2 "$bin"
            cp "$bin" "$work/notarize/$name-$arch"
        done
    done
    ditto -c -k "$work/notarize" "$work/notarize.zip"
    result=$(xcrun notarytool submit "$work/notarize.zip" --keychain-profile "$profile" --wait --timeout 30m --output-format json)
    status=$(jq -r .status <<<"$result")
    if [[ "$status" != Accepted ]]; then
        xcrun notarytool log "$(jq -r .id <<<"$result")" --keychain-profile "$profile" || true
        printf 'notarization ended %s\n' "$status" >&2
        exit 1
    fi
fi
archives=()
native=amd64
[[ "$(uname -m)" != arm64 ]] || native=arm64
for arch in $archs; do
    for variant in full lite; do
        bash "$source/.github/scripts/release-pack.sh" "${tag#v}" "$variant" darwin "$arch" "$dest"
        name=snctl
        [[ "$variant" != lite ]] || name=snctl-lite
        archive="$dest/${name}_${tag#v}_darwin_${arch}.zip"
        if ((!dry)) && [[ "$arch" == "$native" ]]; then
            bash "$source/.github/scripts/smoke-archive.sh" "$archive" "$tag"
        fi
        archives+=("$archive")
    done
done
bash "$source/.github/scripts/checksums.sh" "$dest"
if ((dry)); then
    printf 'packed in %s; nothing uploaded\n' "$dest"
    exit 0
fi
# Preserve every non-macOS checksum verbatim. Download anew on every attempt:
# --clobber repairs partial uploads without duplicating any macOS checksum rows.
gh release download "$tag" --repo "$repo" --pattern checksums.txt --dir "$work" --clobber
awk '$2 !~ /^\*?snctl(-lite)?_.*_darwin_(arm64|amd64)\.zip$/' "$work/checksums.txt" > "$work/checksums.new"
cat "$dest/checksums.txt" >> "$work/checksums.new"
mv "$work/checksums.new" "$work/checksums.txt"
cp "${archives[@]}" "$RELEASE_DIR/"
gh release upload "$tag" --repo "$repo" --clobber "${archives[@]}" "$work/checksums.txt"
cp "$work/checksums.txt" "$RELEASE_DIR/checksums.txt"
if [[ "$tag" == *-* || "$prerelease" == true ]]; then
    printf 'uploaded the macOS archives of %s; a pre-release leaves the Homebrew formulas alone\n' "$tag"
    exit 0
fi
gh workflow run homebrew.yml --repo "$repo" --ref "$tag" -f tag="$tag"
printf 'uploaded the macOS archives of %s and started the Homebrew workflow\n' "$tag"
