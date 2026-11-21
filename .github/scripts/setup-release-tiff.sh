#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Install the release variant only in the disposable Windows packaging runner.
set -euo pipefail
package="$MINGW_PACKAGE_PREFIX-libtiff"
scripts=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"
archive=$(bash "$scripts/msys2-source.sh" "$package" "$work")
tar -xf "$archive"
cd mingw-w64-libtiff
grep -q -- '--enable-jbig' PKGBUILD
sed -i 's/--enable-jbig/--disable-jbig/g' PKGBUILD
# makepkg --syncdeps installs build dependencies through sudo, which MSYS2
# lacks; the runner's shell may install them with pacman directly.
# shellcheck disable=SC1091,SC2154
deps=$(source ./PKGBUILD && printf '%s\n' "${depends[@]:-}" "${makedepends[@]:-}" "${checkdepends[@]:-}")
mapfile -t deps < <(grep -v '^$' <<< "$deps")
pacman -S --needed --noconfirm "${deps[@]}"
MAKEFLAGS="-j$(nproc)" PKGDEST="$PWD" makepkg-mingw --noconfirm
read -r _ version < <(pacman -Q "$package")
pacman -U --noconfirm "$PWD/$package-${version#*:}-"*.pkg.tar.zst
