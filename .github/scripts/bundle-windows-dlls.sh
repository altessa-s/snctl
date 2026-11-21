#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Put the DLLs a Windows snctl.exe loads, and their licences, beside it, or
# check that an unpacked snctl.exe loads nothing else.
#
# Usage: bundle-windows-dlls.sh EXE
#        bundle-windows-dlls.sh --verify EXE
#
# Runs in the MSYS2 CLANG64 or CLANGARM64 shell setup-render-windows.sh ran
# in (called CLANG64 below; CLANGARM64 is the same on ARM64, its packages
# named mingw-w64-clang-aarch64-* after MINGW_PACKAGE_PREFIX, which the
# MSYS2 shell sets). A Windows
# snctl draws with CLANG64's Cairo stack,
# which are DLLs, so the release archive carries them next to snctl.exe,
# where Windows looks first. ntldd -R lists every DLL the program loads,
# directly or through another DLL.
#
# Bundling copies each DLL ntldd resolves under CLANG64's bin into the
# directory of EXE. DLLs under the Windows
# directory are the system's and stay out. A DLL found anywhere else, or not
# found at all (but an API set, which Windows maps itself), fails the script.
# Each CLANG64 DLL belongs to an MSYS2 package (pacman -Qo); the files that
# package installs under $MSYSTEM_PREFIX/share/licenses go to
# licenses/msys2/<package>/, with its pacman -Qi record as PACKAGE.txt
# (version, upstream URL and licence names). A package that installs no
# licence text fails packaging. The names of the copied DLLs are printed.
#
# --verify: ntldd resolves with PATH reduced to the Windows directory, as on a
# machine without MSYS2, and every DLL must come from the directory of EXE or
# from Windows; the DLLs it loads are printed.
set -euo pipefail

scripts=$(cd "$(dirname "$0")" && pwd)
verify=0
if [[ "${1:-}" == --verify ]]; then
    verify=1
    shift
fi
exe=${1:?exe}
[[ -f "$exe" ]] || { printf '%s: no such file\n' "$exe" >&2; exit 1; }
: "${MSYSTEM_PREFIX:?run this in an MSYS2 shell}"
case "${MSYSTEM:-}" in
    CLANGARM64) default_prefix=mingw-w64-clang-aarch64 ;;
    *) default_prefix=mingw-w64-clang-x86_64 ;;
esac
prefix=${MINGW_PACKAGE_PREFIX:-$default_prefix}
out=$(dirname "$exe")
ntldd="$MSYSTEM_PREFIX/bin/ntldd"

# Paths are compared in mixed form, lowercased, with a trailing slash.
lower() {
    local s
    s=$(cat)
    s=${s//\\//}
    printf '%s' "${s,,}"
}
dir_key() { printf '%s/' "$(cygpath -m "$1" | lower)"; }
windows=$(dir_key "${SYSTEMROOT:-C:/Windows}")
if ((verify)); then
    allowed=("$(dir_key "$out")")
    system=$(cygpath -u "${SYSTEMROOT:-C:/Windows}")
    search="$system/System32:$system"
else
    clang_bin=$(dir_key "$MSYSTEM_PREFIX/bin")
    allowed=("$clang_bin")
    if [[ -n "${PDFIUM_BIN:-}" ]]; then allowed+=("$(dir_key "$PDFIUM_BIN")"); fi
    search="${PDFIUM_BIN:+$PDFIUM_BIN:}$PATH"
fi

# ntldd writes "name => path (0xADDRESS)" or "name => not found", with CRLF,
# each DLL indented one step below the module that imports it. A DLL that a
# Windows DLL imports and the machine lacks is Windows' own business (system
# DLLs name optional components, loaded late if at all); only a DLL that
# snctl.exe or a DLL it ships with imports has to be found.
listing=$(PATH="$search" "$ntldd" -R "$exe")
found_re='^([[:space:]]*)([^[:space:]]+) => (.+) \(0x[0-9a-fA-F]+\)[[:space:]]*$'
missing_re='^([[:space:]]*)([^[:space:]]+) => not found'
declare -A packages=()
loaded=()
failed=0
# The indentation of each open module and whether it is a Windows DLL.
depths=()
system_parent=()
# parent_is_system INDENT: whether the module that imports a DLL indented
# INDENT deep is a Windows DLL; closes the modules indented as deep or deeper.
parent_is_system() {
    while ((${#depths[@]} > 0)) && ((depths[-1] >= $1)); do
        unset 'depths[-1]' 'system_parent[-1]'
    done
    ((${#depths[@]} > 0)) && ((system_parent[-1]))
}
while IFS= read -r line; do
    if [[ "$line" =~ $missing_re ]]; then
        indent=${#BASH_REMATCH[1]}
        name=${BASH_REMATCH[2]}
        [[ "$name" =~ ^(api|ext)-ms- ]] && continue
        parent_is_system "$indent" && continue
        printf '::error::%s needs %s, which is not found\n' "$exe" "$name" >&2
        failed=1
        continue
    fi
    [[ "$line" =~ $found_re ]] || continue
    indent=${#BASH_REMATCH[1]}
    name=${BASH_REMATCH[2]}
    path=${BASH_REMATCH[3]}
    if [[ "${name,,}" == *jbig*.dll ]]; then
        printf '::error::%s imports %s; rebuild libtiff without JBIG\n' "$exe" "$name" >&2
        exit 1
    fi
    where=$(printf '%s' "$path" | lower)
    parent_is_system "$indent" || true
    depths+=("$indent")
    if [[ "$where" == "$windows"* ]]; then
        system_parent+=(1)
        continue
    fi
    system_parent+=(0)
    ok=0
    for dir in "${allowed[@]}"; do
        [[ "$where" == "$dir"* ]] && ok=1
    done
    if ((!ok)); then
        printf '::error::%s loads %s from %s, outside %s\n' "$exe" "$name" "$path" "${allowed[*]}" >&2
        failed=1
        continue
    fi
    unix=$(cygpath -u "$path")
    loaded+=("$(basename "$unix")")
    ((verify)) && continue
    cp "$unix" "$out/"
    if [[ "$where" == "$clang_bin"* ]]; then
        packages[$(pacman -Qqo "$unix")]=1
    fi
done <<<"$listing"
((failed == 0)) || exit 1
((${#loaded[@]} > 0)) || { printf '::error::ntldd lists no DLL of %s\n' "$exe" >&2; exit 1; }

for package in "${!packages[@]}"; do
    dest="$out/licenses/msys2/${package#"$prefix"-}"
    mkdir -p "$dest"
    LC_ALL=C pacman -Qi "$package" > "$dest/PACKAGE.txt"
    # Split binary packages can share a single corresponding source package.
    if grep -iE '^Licenses[[:space:]]*:.*LGPL' "$dest/PACKAGE.txt" > /dev/null; then
        archive=$(bash "$scripts/msys2-source.sh" "$package" "$dest")
        printf '%s is dynamically linked. Its corresponding source, MSYS2 patches and build recipe are in %s beside this notice.\n' \
            "$package" "$(basename "$archive")" > "$dest/SOURCE.txt"
    fi
    found=0
    while IFS= read -r file; do
        [[ "$file" == "$MSYSTEM_PREFIX/share/licenses/"* && -f "$file" ]] || continue
        # The path below the package's own licences directory.
        rel=${file#"$MSYSTEM_PREFIX/share/licenses/"}
        rel=${rel#*/}
        mkdir -p "$dest/$(dirname "$rel")"
        cp "$file" "$dest/$rel"
        found=1
    done < <(pacman -Qlq "$package")
    ((found)) || { printf '::error::%s installs no licence text\n' "$package" >&2; exit 1; }
done

printf '%s\n' "${loaded[@]}" | sort -fu
