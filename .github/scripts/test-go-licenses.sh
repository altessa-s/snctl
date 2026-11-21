#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Run the real CLI on UTF-8 go list output with ASCII and cp1252 defaults.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
python3 - "$root" "$work" <<'PYTHON'
import json
import os
from pathlib import Path
import subprocess
import sys

root, work = map(Path, sys.argv[1:])
module = work / "module"
module.mkdir()
notice = "Copyright José — Ё\n".encode("utf-8")
(module / "LICENSE").write_bytes(notice)
goroot = work / "goroot"
goroot.mkdir()
(goroot / "LICENSE").write_bytes(b"Go licence\r\n")
# JSON with literal non-ASCII, multiple package records and CRLF. UTF-8's
# second byte for Ё is undefined in cp1252, reproducing the release failure.
packages = [
    {"Standard": True, "Root": str(goroot), "Doc": "Ё — café"},
    {"Module": {"Path": "example.org/linked", "Version": "v1.0.0", "Dir": str(module)}},
]
fixture = work / "go-packages.json"
fixture.write_bytes(("\r\n".join(json.dumps(p, ensure_ascii=False) for p in packages) + "\r\n").encode("utf-8"))
# Patch only the implicit text encoding. Explicit encoding='utf-8' must win.
# This exercises __main__ (not just collect) through Path.read_text/io.open.
wrapper = r'''
import io
from pathlib import Path
import runpy
import sys
from unittest.mock import patch
assert sys.flags.utf8_mode == 0
original_open = io.open
encoding = sys.argv.pop(1)
def legacy_open(file, mode="r", buffering=-1, encoding=None, *args, **kwargs):
    if "b" not in mode and encoding is None:
        encoding = default_encoding
    return original_open(file, mode, buffering, encoding, *args, **kwargs)
default_encoding = encoding
with patch("io.open", legacy_open):
    try:
        Path(sys.argv[2]).read_text()
    except UnicodeDecodeError:
        pass
    else:
        raise AssertionError("fixture did not reject the simulated legacy encoding")
    sys.argv = sys.argv[1:]
    runpy.run_path(sys.argv[0], run_name="__main__")
'''
env = dict(os.environ, PYTHONUTF8="0", PYTHONCOERCECLOCALE="0", LC_ALL="C", PYTHONDONTWRITEBYTECODE="1")
for encoding in ("ascii", "cp1252"):
    destination = work / encoding
    subprocess.run([sys.executable, "-c", wrapper, encoding,
                    str(root / ".github/scripts/go-licenses.py"), str(fixture), str(destination)],
                   env=env, check=True)
    assert (destination / "example.org/linked@v1.0.0/LICENSE").read_bytes() == notice
    assert (destination / "go/LICENSE").read_bytes() == b"Go licence\r\n"
PYTHON
echo 'UTF-8 go list CLI passed with ASCII/cp1252 defaults and PYTHONUTF8=0.'
