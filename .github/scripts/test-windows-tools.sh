#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Exercise Windows dependency and licence checks with tool fixtures in Bash 5.
# This does not execute a Windows binary or access the host's system libraries.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$root/bin/.tmp"
work=$(mktemp -d "$root/bin/.tmp/windows tools.XXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/toolchain/bin" "$work/toolchain/share/licenses/fake" "$work/out" "$work/stubs" "$work/windows/System32" "$work/pdfium/bin"
printf 'binary' > "$work/out/snctl.exe"
printf 'library' > "$work/toolchain/bin/libcairo-2.dll"
printf 'JBIG fixture' > "$work/toolchain/bin/libjbig-0.dll"
printf 'PDFium fixture' > "$work/pdfium/bin/pdfium.dll"
printf 'MIT licence fixture' > "$work/toolchain/share/licenses/fake/LICENSE"
cat > "$work/stubs/cygpath" <<'STUB'
#!/bin/bash
printf '%s\n' "$2"
STUB
cat > "$work/toolchain/bin/ntldd" <<'STUB'
#!/bin/bash
if [[ "${VERIFY_TEST:-}" == 1 ]]; then
    printf '  libcairo-2.dll => %s/libcairo-2.dll (0x123)\r\n' "$TOOL_TEST_OUT"
    printf '  pdfium.dll => %s/pdfium.dll (0x456)\r\n' "$TOOL_TEST_OUT"
elif [[ "${SCENARIO:-}" == unknown ]]; then
    printf '  evil.dll => /outside/evil.dll (0x123)\r\n'
elif [[ "${SCENARIO:-}" == jbig ]]; then
    printf '  libjbig-0.dll => %s/bin/libjbig-0.dll (0x123)\r\n' "$MSYSTEM_PREFIX"
elif [[ "${SCENARIO:-}" == missing ]]; then
    printf '  evil.dll => not found\r\n'
else
    printf '  libcairo-2.dll => %s/bin/libcairo-2.dll (0x123)\r\n' "$MSYSTEM_PREFIX"
    printf '  pdfium.dll => %s/pdfium.dll (0x456)\r\n' "$PDFIUM_BIN"
fi
STUB
cat > "$work/stubs/pacman" <<'STUB'
#!/bin/bash
case "$1" in
    -Qqo) printf 'mingw-w64-clang-aarch64-cairo\n' ;;
    -Qi) printf 'Name : cairo\nLicenses : %s\n' "${FIXTURE_LICENSE:-MIT}" ;;
    -Q) printf '%s 1.2-3\n' "$2" ;;
    -Qlq) printf '%s/share/licenses/fake/LICENSE\n' "$MSYSTEM_PREFIX" ;;
esac
STUB
cat > "$work/stubs/pacman-conf" <<'STUB'
#!/bin/bash
printf '%s/db\n' "$MSYSTEM_PREFIX"
STUB
cat > "$work/stubs/curl" <<'STUB'
#!/bin/bash
case "$4" in
    https://repo.msys2.org/mingw/sources/mingw-w64-test-base-1.2-3.src.tar.zst*) ;;
    *) exit 1 ;;
esac
printf 'source fixture\n' > "$6"
STUB
cat > "$work/stubs/gpgv" <<'STUB'
#!/bin/bash
[[ "$1" == --keyring && "$2" == */pubring.gpg && -s "$3" && -s "$4" && -z "${BAD_SIGNATURE:-}" ]]
STUB
cat > "$work/stubs/llvm-objdump" <<'STUB'
#!/bin/bash
printf '    DLL Name: kernel32.dll\r\n'
if [[ "${SCENARIO:-}" == unknown ]]; then
    printf '    DLL Name: evil.dll\n'
fi
STUB
chmod +x "$work"/stubs/* "$work/toolchain/bin/ntldd"
export PATH="$work/stubs:$PATH" MSYSTEM_PREFIX="$work/toolchain" MSYSTEM=CLANGARM64 SYSTEMROOT="$work/windows" PDFIUM_BIN="$work/pdfium/bin"
bash "$root/.github/scripts/bundle-windows-dlls.sh" "$work/out/snctl.exe"
test -s "$work/out/libcairo-2.dll"
test -s "$work/out/pdfium.dll"
test -s "$work/out/licenses/msys2/cairo/LICENSE"
VERIFY_TEST=1 TOOL_TEST_OUT="$work/out" bash "$root/.github/scripts/bundle-windows-dlls.sh" --verify "$work/out/snctl.exe"
package=mingw-w64-clang-aarch64-cairo
record="$MSYSTEM_PREFIX/db/local/$package-1.2-3"
mkdir -p "$record"
printf '%%BASE%%\nmingw-w64-test-base\n' > "$record/desc"
export MINGW_PACKAGE_PREFIX=mingw-w64-clang-aarch64
FIXTURE_LICENSE=spdx:LGPL-2.1-or-later bash "$root/.github/scripts/bundle-windows-dlls.sh" "$work/out/snctl.exe"
dest="$work/out/licenses/msys2/cairo"
test -s "$dest/mingw-w64-test-base-1.2-3.src.tar.zst"
grep -q 'mingw-w64-test-base-1.2-3.src.tar.zst' "$dest/SOURCE.txt"
if BAD_SIGNATURE=1 FIXTURE_LICENSE=LGPL bash "$root/.github/scripts/bundle-windows-dlls.sh" "$work/out/snctl.exe"; then
    echo 'accepted an unverified source archive' >&2
    exit 1
fi
for scenario in unknown missing jbig; do
    if SCENARIO=$scenario bash "$root/.github/scripts/bundle-windows-dlls.sh" "$work/out/snctl.exe"; then
        exit 1
    fi
done
rm "$work/toolchain/share/licenses/fake/LICENSE"
if bash "$root/.github/scripts/bundle-windows-dlls.sh" "$work/out/snctl.exe"; then
    exit 1
fi
printf 'Windows DLL fixture' > "$work/windows/System32/kernel32.dll"
bash "$root/.github/scripts/check-windows-static.sh" "$work/out/snctl.exe"
if SCENARIO=unknown bash "$root/.github/scripts/check-windows-static.sh" "$work/out/snctl.exe"; then
    exit 1
fi
echo 'Windows tool fixtures passed; native Windows execution remains required.'
