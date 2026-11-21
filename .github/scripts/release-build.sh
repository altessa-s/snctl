#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Build and stage one full or lite target. Dry runs perform the same build and
# licence checks, and never sign, access credentials or publish anything.
set -euo pipefail
sha256="$(cd "$(dirname "$0")" && pwd)/sha256.sh"
if [[ "${1:-}" == --dry-run ]]; then
    echo 'Dry run: building locally without signing or publication.' >&2
    shift
fi
version=${1:?version or tag}
version=${version#v}
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo 'invalid version' >&2; exit 2; }
variant=${2:?full or lite}
case "$variant" in full | lite) ;; *) exit 2 ;; esac
os=${GOOS:-$(go env GOOS)}
arch=${GOARCH:-$(go env GOARCH)}
case "$os/$arch" in darwin/arm64 | darwin/amd64 | linux/arm64 | linux/amd64 | windows/arm64 | windows/amd64) ;; *) exit 2 ;; esac
root=$(cd "$(dirname "$0")/../.." && pwd)
dest=${RELEASE_DIR:-$root/bin/release}
mkdir -p "$dest"
dest=$(cd "$dest" && pwd)
name=snctl
cgo=1
tags=''
if [[ "$variant" == lite ]]; then
    name=snctl-lite
    tags=norender
fi
out="$dest/stage/$name-$os-$arch"
if [[ -e "$out" ]]; then
    echo "stage already exists: $out; choose a new RELEASE_DIR" >&2
    exit 1
fi
mkdir -p "$out"
binary=snctl
[[ "$os" != windows ]] || binary=snctl.exe
pkg=github.com/altessa-s/go-atlas-core/runtime/appinfo
commit=$(git rev-parse --short=7 HEAD)
built=${SOURCE_DATE_EPOCH:-}
if [[ -n "$built" ]]; then
    built=$(python3 -c 'import datetime,sys; print(datetime.datetime.fromtimestamp(int(sys.argv[1]),datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))' "$built")
else
    built=$(date -u +%Y-%m-%dT%H:%M:%SZ)
fi
linkflags="-s -w -X $pkg.Name=snctl -X $pkg.Project=snctl -X $pkg.Version=$version -X $pkg.Commit=$commit -X $pkg.BuildTime=$built"
if [[ "$os" == windows && "$variant" == lite ]]; then
    linkflags="$linkflags -linkmode=external -extldflags=-static"
fi
# Apple uses system frameworks. Lite never fetches or links PDFium.
if [[ "$variant" == full && "$os" != darwin ]]; then
    renderer=$(go list -m -f '{{.Dir}}' github.com/altessa-s/snrender-go)
    pdfium=${PDFIUM_DIR:-$dest/pdfium-$os-$arch}
    platform=linux-x64
    [[ "$arch" != arm64 ]] || platform=linux-arm64
    if [[ "$os" == windows ]]; then
        renderer=$(cygpath -m "$renderer")
        pdfium=$(cygpath -m "$pdfium")
        platform=win-x64
        [[ "$arch" != arm64 ]] || platform=win-arm64
    fi
    # Setup jobs may have fetched this SDK already. Standalone builds fetch it here.
    if [[ -z "${PDFIUM_DIR:-}" ]]; then
        PYTHONIOENCODING=utf-8 python3 "$renderer/internal/clib/snrender/scripts/fetch-pdfium.py" --platform "$platform" --output "$pdfium"
    fi
    export CGO_CFLAGS="${CGO_CFLAGS:-} \"-I$pdfium/include\""
    export CGO_LDFLAGS="${CGO_LDFLAGS:-} \"-L$pdfium/lib\""
    if [[ "$os" == linux ]]; then
        export CGO_LDFLAGS="$CGO_LDFLAGS -Wl,-rpath,\$ORIGIN"
    else
        PATH="$(cygpath -u "$pdfium")/bin:$PATH"
        export PATH
    fi
fi
CGO_ENABLED=$cgo GOOS=$os GOARCH=$arch go build -trimpath -tags "$tags" -o "$out/$binary" -ldflags "$linkflags" ./cmd/snctl
CGO_ENABLED=$cgo GOOS=$os GOARCH=$arch go list -deps -json -tags "$tags" ./cmd/snctl > "$out/go-packages.json"
python3 "$root/.github/scripts/go-licenses.py" "$out/go-packages.json" "$out/licenses/go"
rm "$out/go-packages.json"
cp LICENSE COPYRIGHT README.md "$out/"
if [[ "$cgo" == 1 ]]; then
    reader=$(go list -m -f '{{.Dir}}' github.com/altessa-s/snnote-go)
    if [[ "$os" == windows ]]; then
        reader=$(cygpath -u "$reader")
    fi
    mkdir -p "$out/licenses/snnote-lib" "$out/licenses/snnote-go"
    cp "$reader/LICENSE" "$out/licenses/snnote-go/"
    cp "$reader/internal/clib/LICENSE" "$out/licenses/snnote-lib/"
fi
if [[ "$variant" == full ]]; then
    renderer=$(go list -m -f '{{.Dir}}' github.com/altessa-s/snrender-go)
    if [[ "$os" == windows ]]; then
        renderer=$(cygpath -u "$renderer")
    fi
    mkdir -p "$out/licenses/snrender-lib" "$out/licenses/snrender-go"
    cp "$renderer/LICENSE" "$out/licenses/snrender-go/"
    cp "$renderer/internal/clib/snrender/LICENSE" "$out/licenses/snrender-lib/"
    # Preserve every vendored notice, including font licences added by later bindings.
    while IFS= read -r notice; do
        relative=${notice#"$renderer/"}
        mkdir -p "$out/licenses/snrender-go/$(dirname "$relative")"
        cp "$notice" "$out/licenses/snrender-go/$relative"
    done < <(find "$renderer/internal/clib" -type f \( -iname '*license*' -o -iname '*copyright*' -o -iname '*ofl*' \))
    if [[ -d "$renderer/fonts" ]]; then
        mkdir -p "$out/licenses/fonts"
        cp "$renderer"/fonts/*-OFL.txt "$out/licenses/fonts/"
    fi
    if [[ "$os" != darwin ]]; then
        mkdir -p "$out/licenses/pdfium"
        test -s "$pdfium/LICENSE"
        test -d "$pdfium/licenses"
        cp "$pdfium/LICENSE" "$out/licenses/pdfium/"
        cp -R "$pdfium/licenses" "$out/licenses/pdfium/"
        test -n "$(find "$out/licenses/pdfium/licenses" -type f -print -quit)"
        (cd "$out/licenses/pdfium" && find LICENSE licenses -type f -exec bash "$sha256" {} + > notices.sha256)
        if [[ "$os" == linux ]]; then
            cp "$pdfium/lib/libpdfium.so" "$out/"
        else
            PDFIUM_BIN="$(cygpath -u "$pdfium")/bin"
            export PDFIUM_BIN
        fi
    fi
    if [[ "$os" == windows ]]; then
        bash "$root/.github/scripts/bundle-windows-dlls.sh" "$out/$binary"
    fi
    for component in snnote-lib snrender-lib snnote-go snrender-go; do
        test -s "$out/licenses/$component/LICENSE"
    done
    if find "$out/licenses" -type f -empty | grep -q .; then
        echo 'empty licence in package' >&2
        exit 1
    fi
fi
(cd "$out" && find licenses -type f -exec bash "$sha256" {} + > licenses.sha256)
printf '%s\n' "$out"
if [[ "${RELEASE_STAGE_ONLY:-0}" != 1 ]]; then
    bash "$root/.github/scripts/release-pack.sh" "$version" "$variant" "$os" "$arch" "$dest"
fi
