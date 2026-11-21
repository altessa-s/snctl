#!/usr/bin/env python3
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
"""Copy notices from precisely the modules selected by go list -deps -json."""
import json
from pathlib import Path
import shutil
import sys


def collect(source, destination):
    decoder = json.JSONDecoder()
    modules = {}
    goroot = None
    while source.strip():
        package, end = decoder.raw_decode(source.lstrip())
        source = source.lstrip()[end:]
        if package.get("Standard") and package.get("Root"):
            goroot = Path(package["Root"])
        module = package.get("Module")
        if module and not module.get("Main"):
            modules[module["Path"]] = module
    # The runtime and standard library belong to no module but are linked into
    # every binary, so Go's own licence travels with them.
    # The official distribution keeps LICENSE in GOROOT; Homebrew moves it one
    # level up.
    if goroot is not None:
        found = [p for p in (goroot / "LICENSE", goroot.parent / "LICENSE")
                 if p.is_file() and p.stat().st_size > 0]
        if not found:
            raise SystemExit(f"missing licence text for the Go standard library at {goroot}")
        (destination / "go").mkdir(parents=True)
        shutil.copyfile(found[0], destination / "go" / "LICENSE")
        patents = goroot / "PATENTS"
        if patents.is_file():
            shutil.copyfile(patents, destination / "go" / "PATENTS")
    for name, module in sorted(modules.items()):
        actual = module.get("Replace", module)
        directory = Path(actual["Dir"])
        notices = [file for file in directory.iterdir()
                   if file.is_file() and file.name.upper().startswith(
                       ("LICENSE", "LICENCE", "COPYING", "NOTICE"))]
        if not notices or any(file.stat().st_size == 0 for file in notices):
            raise SystemExit(f"missing licence text for linked module {name}")
        target = destination / (name + "@" + actual.get("Version", "local"))
        target.mkdir(parents=True)
        for file in notices:
            shutil.copyfile(file, target / file.name)
    if not modules:
        raise SystemExit("no linked modules in package listing")


if __name__ == "__main__":
    # go list writes UTF-8; Windows' default code page cannot read it.
    collect(Path(sys.argv[1]).read_text(encoding="utf-8"), Path(sys.argv[2]))
