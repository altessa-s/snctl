#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Fetch the signed source package matching an installed MSYS2 binary package.
set -euo pipefail
package=${1:?installed package}
dest=${2:?destination directory}
read -r _ version < <(pacman -Q "$package")
record="$(pacman-conf DBPath)/local/$package-$version/desc"
base=$(awk '/^%BASE%$/ {getline; print; exit}' "$record")
# MSYS2 source packages use the architecture-independent mingw-w64 prefix.
base=${base:-$package}
base=${base/#"$MINGW_PACKAGE_PREFIX"-/mingw-w64-}
archive="$base-${version#*:}.src.tar.zst"
url="https://repo.msys2.org/mingw/sources/$archive"
mkdir -p "$dest"
curl -fsSL --retry 3 "$url" -o "$dest/$archive"
curl -fsSL --retry 3 "$url.sig" -o "$dest/$archive.sig"
# pacman-key --verify demands administrator rights the runner's MSYS2 shell
# lacks; gpgv reads the same pacman keyring without writing to it.
keyring="$(pacman-conf GPGDir)/pubring.gpg"
gpgv --keyring "$keyring" "$dest/$archive.sig" "$dest/$archive" >&2
printf '%s\n' "$dest/$archive"
