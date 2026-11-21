#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Pack a staged build only after checking the required redistribution notices.
set -euo pipefail
sha256="$(cd "$(dirname "$0")" && pwd)/sha256.sh"
version=${1:?version}
variant=${2:?variant}
os=${3:?os}
arch=${4:?arch}
dest=${5:?release directory}
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || exit 2
case "$variant" in full | lite) ;; *) exit 2 ;; esac
case "$os/$arch" in darwin/arm64 | darwin/amd64 | linux/arm64 | linux/amd64 | windows/arm64 | windows/amd64) ;; *) exit 2 ;; esac
name=snctl
[[ "$variant" != lite ]] || name=snctl-lite
out="$dest/stage/$name-$os-$arch"
binary=snctl
[[ "$os" != windows ]] || binary=snctl.exe
for file in LICENSE COPYRIGHT README.md "$binary"; do
    test -s "$out/$file"
done
test -s "$out/licenses.sha256"
test -d "$out/licenses/go"
(cd "$out" && bash "$sha256" -c licenses.sha256 > /dev/null)
if find "$out/licenses" -type f -empty | grep -q .; then
    echo 'empty licence in package' >&2
    exit 1
fi
for component in snnote-lib snnote-go; do
    test -s "$out/licenses/$component/LICENSE"
done
if [[ "$variant" == full ]]; then
    for component in snnote-lib snrender-lib snnote-go snrender-go; do
        test -s "$out/licenses/$component/LICENSE"
    done
    if [[ "$os" != darwin ]]; then
        test -s "$out/licenses/pdfium/LICENSE"
        test -s "$out/licenses/pdfium/notices.sha256"
        test -n "$(find "$out/licenses/pdfium/licenses" -type f -print -quit)"
        (cd "$out/licenses/pdfium" && bash "$sha256" -c notices.sha256 > /dev/null)
        if [[ "$os" == linux ]]; then test -s "$out/libpdfium.so"; else test -s "$out/pdfium.dll"; fi
    fi
    if [[ "$os" == windows ]]; then
        test -d "$out/licenses/msys2"
        if [[ -n "$(find "$out" -iname '*jbig*.dll' -print -quit)" ]]; then
            echo 'Windows archive contains a JBIG DLL' >&2
            exit 1
        fi
        while IFS= read -r package; do
            if ! find "$package" -type f ! -name PACKAGE.txt | grep -q .; then
                echo "missing licence text: $package" >&2
                exit 1
            fi
        done < <(find "$out/licenses/msys2" -mindepth 1 -maxdepth 1 -type d)
    fi
fi
archive="$dest/${name}_${version}_${os}_${arch}"
if [[ "$os" == linux ]]; then
    COPYFILE_DISABLE=1 tar --no-xattrs -C "$out" -czf "$archive.tar.gz" .
    printf '%s\n' "$archive.tar.gz"
else
    rm -f "$archive.zip"
    (cd "$out" && zip -q -r "$archive.zip" .)
    printf '%s\n' "$archive.zip"
fi
