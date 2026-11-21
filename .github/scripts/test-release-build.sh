#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Exercise cgo environment construction without compiling or fetching an SDK.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/tools" "$work/SDK with spaces"
cat > "$work/tools/go" <<'STUB'
#!/bin/bash
if [[ "$1" == list ]]; then echo "$FAKE_RENDERER"; exit; fi
if [[ "$1" == build ]]; then
    python3 - "$CGO_CFLAGS" "$CGO_LDFLAGS" > "$FLAGS_CAPTURE" <<'PYTHON'
import json, sys
json.dump(sys.argv[1:], sys.stdout)
PYTHON
    exit 79
fi
exit 93
STUB
chmod +x "$work/tools/go"
result=0
PATH="$work/tools:$PATH" GOOS=linux GOARCH=amd64 PDFIUM_DIR="$work/SDK with spaces" \
    RELEASE_DIR="$work/release" FAKE_RENDERER="$work/renderer" FLAGS_CAPTURE="$work/flags.json" \
    CGO_CFLAGS=-DKEEP_CFLAGS CGO_LDFLAGS=-Wl,--as-needed \
    bash "$root/.github/scripts/release-build.sh" v9.8.7 full > "$work/log" 2>&1 || result=$?
[[ "$result" == 79 ]] || { cat "$work/log"; exit 1; }
python3 - "$work/flags.json" "$work/SDK with spaces" <<'PYTHON'
import json, shlex, sys
cflags, ldflags = map(shlex.split, json.load(open(sys.argv[1])))
sdk = sys.argv[2]
assert cflags == ['-DKEEP_CFLAGS', '-I' + sdk + '/include'], cflags
assert ldflags == ['-Wl,--as-needed', '-L' + sdk + '/lib', '-Wl,-rpath,$ORIGIN'], ldflags
PYTHON
echo 'Release cgo flags preserve caller flags and SDK paths with spaces.'

# Setup must also quote flags before release-build appends its own flags. A
# quoted second -I/-L cannot repair an earlier unquoted path with spaces.
mkdir -p "$work/renderer/internal/clib/snrender/scripts"
cat > "$work/renderer/internal/clib/snrender/scripts/fetch-pdfium.py" <<'PYTHON'
import sys
assert sys.stdout.encoding.lower().replace('-', '') == 'utf8'
print('SDK ready: \u0401')
assert sys.argv[1] == '--platform'
assert sys.argv[3:] == ['--output', sys.argv[4]]
PYTHON
cat > "$work/tools/uname" <<'STUB'
#!/bin/bash
case "$1" in
    -s) echo "$FAKE_SYSTEM" ;;
    -m) echo x86_64 ;;
    *) exit 93 ;;
esac
STUB
cat > "$work/tools/cygpath" <<'STUB'
#!/bin/bash
# Model a native mixed-form SDK path while keeping the Python fixture local.
if [[ "$2" == "$FAKE_RENDERER" ]]; then
    echo "$2"
elif [[ "$1" == -m ]]; then
    echo 'C:/Users/Runner Name/SDK with spaces'
else
    echo '/c/Users/Runner Name/SDK with spaces'
fi
STUB
chmod +x "$work/tools/uname" "$work/tools/cygpath"
for system in Linux MINGW64_NT; do
    PATH="$work/tools:$PATH" FAKE_SYSTEM="$system" MSYSTEM=CLANG64 \
        FAKE_RENDERER="$work/renderer" PDFIUM_DIR="$work/SDK with spaces" \
        PYTHONIOENCODING=cp1252 CGO_CFLAGS=-DKEEP_CFLAGS CGO_LDFLAGS=-Wl,--as-needed GITHUB_ENV="$work/$system.env" \
        bash "$root/.github/scripts/setup-pdfium.sh" '$ORIGIN'
    python3 - "$work/$system.env" "$system" "$work/SDK with spaces" <<'PYTHON'
from pathlib import Path
import shlex
import sys
values = dict(line.split('=', 1) for line in Path(sys.argv[1]).read_text(encoding='utf-8').splitlines())
windows = sys.argv[2] != 'Linux'
sdk = 'C:/Users/Runner Name/SDK with spaces' if windows else sys.argv[3]
assert values['PDFIUM_DIR'] == sdk, values
assert shlex.split(values['CGO_CFLAGS']) == ['-DKEEP_CFLAGS', '-I' + sdk + '/include'], values
expected = ['-Wl,--as-needed', '-L' + sdk + '/lib']
if windows:
    assert values['PDFIUM_BIN'] == '/c/Users/Runner Name/SDK with spaces/bin', values
else:
    expected += ['-Wl,-rpath,$ORIGIN']
assert shlex.split(values['CGO_LDFLAGS']) == expected, values
PYTHON
done
echo 'PDFium setup preserves flags, paths with spaces and mixed Windows paths.'

# Standalone builds call the same fetcher without setup-pdfium/GITHUB_ENV.
result=0
PATH="$work/tools:$PATH" GOOS=linux GOARCH=amd64 PDFIUM_DIR='' \
    RELEASE_DIR="$work/standalone" FAKE_RENDERER="$work/renderer" FLAGS_CAPTURE="$work/standalone.json" \
    PYTHONIOENCODING=cp1252 \
    bash "$root/.github/scripts/release-build.sh" --dry-run v9.8.7 full > "$work/standalone.log" 2>&1 || result=$?
[[ "$result" == 79 ]] || { cat "$work/standalone.log"; exit 1; }
echo 'PDFium helper logs use UTF-8 even with a legacy inherited stdout encoding.'
