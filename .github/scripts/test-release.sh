#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
# Render both package managers from a synthetic release and reject missing hashes.
set -euo pipefail
sha256="$(cd "$(dirname "$0")" && pwd)/sha256.sh"
root=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$root/bin/.tmp"
work=$(mktemp -d "$root/bin/.tmp/release-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
version=9.8.7
for name in snctl snctl-lite; do
    for os in darwin linux windows; do
        extension=zip
        [[ "$os" != linux ]] || extension=tar.gz
        for arch in amd64 arm64; do
            printf '%064d  %s_%s_%s_%s.%s\n' 1 "$name" "$version" "$os" "$arch" "$extension" >> "$work/checksums.txt"
        done
    done
done
if [[ -n "${1:-}" ]]; then
    python3 - "$work/checksums.txt" "$1" <<'PYTHON'
from pathlib import Path
import sys
base, extra = map(Path, sys.argv[1:])
values = {line.split()[1]: line.split()[0] for line in base.read_text().splitlines()}
for line in extra.read_text().splitlines():
    digest, name = line.split()
    if name in values:
        values[name] = digest
base.write_text("".join(f"{value}  {name}\n" for name, value in values.items()))
PYTHON
fi
for variant in full lite; do
    flags=()
    [[ "$variant" != lite ]] || flags=(--lite)
    bash "$root/.github/scripts/homebrew-formula.sh" ${flags[@]+"${flags[@]}"} "$version" "$work/checksums.txt" > "$work/$variant.rb"
    bash "$root/.github/scripts/scoop-manifest.sh" ${flags[@]+"${flags[@]}"} "$version" "$work/checksums.txt" > "$work/$variant.json"
    ruby -c "$work/$variant.rb"
    python3 - "$work/$variant.json" "$work/checksums.txt" <<'PYTHON'
import json, sys
manifest = json.load(open(sys.argv[1]))
sums = {line.split()[1]: line.split()[0] for line in open(sys.argv[2])}
assert manifest["version"] == "9.8.7"
assert set(manifest["architecture"]) == {"64bit", "arm64"}
assert manifest["bin"] == "snctl.exe"
for item in manifest["architecture"].values():
    assert item["hash"] == sums[item["url"].rsplit("/", 1)[1]]
PYTHON
    python3 - "$work/$variant.rb" "$work/checksums.txt" "$variant" <<'PYTHON'
import re, sys
text = open(sys.argv[1]).read()
sums = {line.split()[1]: line.split()[0] for line in open(sys.argv[2])}
entries = re.findall(r'url "[^"\n]*/([^/"\n]+)"\s+sha256 "([0-9a-f]{64})"', text)
assert len(entries) == 4
for name, digest in entries:
    assert sums[name] == digest
assert ('depends_on "cairo"' in text) == (sys.argv[3] == 'full')
assert 'conflicts_with "snctl' in text
PYTHON
    grep -q 'altessa-s/snctl/releases' "$work/$variant.rb"
    grep -q 'class Snctl' "$work/$variant.rb"
    if bash "$root/.github/scripts/homebrew-formula.sh" ${flags[@]+"${flags[@]}"} "$version" /dev/null > "$work/invalid" 2>/dev/null; then
        echo 'formula accepted missing checksums' >&2
        exit 1
    fi
    test ! -s "$work/invalid"
    if bash "$root/.github/scripts/scoop-manifest.sh" ${flags[@]+"${flags[@]}"} "$version" /dev/null > "$work/invalid" 2>/dev/null; then
        echo 'manifest accepted missing checksums' >&2
        exit 1
    fi
    test ! -s "$work/invalid"
    if [[ -n "${RELEASE_TEST_OUTPUT:-}" ]]; then
        mkdir -p "$RELEASE_TEST_OUTPUT"
        cp "$work/$variant.rb" "$work/$variant.json" "$work/checksums.txt" "$RELEASE_TEST_OUTPUT/"
    fi
done
echo 'Fake-release formulas, manifests and missing-hash refusals passed.'
# The packer must refuse a missing licence before writing any archive.
stage="$work/stage/snctl-darwin-arm64"
mkdir -p "$stage/licenses/go/fixture"
printf 'Go module licence fixture\n' > "$stage/licenses/go/fixture/LICENSE"
for file in snctl LICENSE COPYRIGHT README.md; do
    printf 'fixture\n' > "$stage/$file"
done
for component in snnote-lib snrender-lib snnote-go snrender-go; do
    mkdir -p "$stage/licenses/$component"
    printf 'fixture licence\n' > "$stage/licenses/$component/LICENSE"
done
(cd "$stage" && find licenses -type f -exec bash "$sha256" {} + > licenses.sha256)
bash "$root/.github/scripts/release-pack.sh" "$version" full darwin arm64 "$work" > /dev/null
rm "$work/snctl_${version}_darwin_arm64.zip"
for component in snnote-lib snrender-lib snnote-go snrender-go; do
    mv "$stage/licenses/$component/LICENSE" "$work/notice"
    if bash "$root/.github/scripts/release-pack.sh" "$version" full darwin arm64 "$work" > /dev/null 2>&1; then
        echo "packer accepted missing $component licence" >&2
        exit 1
    fi
    test ! -e "$work/snctl_${version}_darwin_arm64.zip"
    mv "$work/notice" "$stage/licenses/$component/LICENSE"
done
echo 'Licence packaging and missing-notice refusals passed.'

# Lite archives must carry their Go modules and native reader notices too.
cp -R "$stage" "$work/stage/snctl-lite-darwin-arm64"
lite_stage="$work/stage/snctl-lite-darwin-arm64"
bash "$root/.github/scripts/release-pack.sh" "$version" lite darwin arm64 "$work" > /dev/null
rm "$work/snctl-lite_${version}_darwin_arm64.zip"
rm "$lite_stage/licenses/go/fixture/LICENSE"
if bash "$root/.github/scripts/release-pack.sh" "$version" lite darwin arm64 "$work" > /dev/null 2>&1; then
    echo 'lite packer accepted a missing Go module licence' >&2
    exit 1
fi
python3 - "$root" "$work" <<'PYTHON'
import importlib.util
import json
from pathlib import Path
import sys
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("licenses", Path(sys.argv[1]) / ".github/scripts/go-licenses.py")
licenses = importlib.util.module_from_spec(spec)
spec.loader.exec_module(licenses)
work = Path(sys.argv[2])
module = work / "module"
module.mkdir()
(module / "LICENSE").write_text("fixture licence")
(module / "NOTICE.txt").write_text("fixture notice")
linked = {"Module": {"Path": "example.org/linked", "Version": "v1.0.0", "Dir": str(module)}}
licenses.collect(json.dumps(linked) + json.dumps(linked) + json.dumps({"Standard": True}), work / "collected")
assert sorted(p.name for p in (work / "collected/example.org/linked@v1.0.0").iterdir()) == ["LICENSE", "NOTICE.txt"]
(module / "LICENSE").unlink()
(module / "NOTICE.txt").unlink()
try:
    licenses.collect(json.dumps(linked), work / "missing")
except SystemExit as error:
    assert "missing licence text" in str(error)
else:
    raise AssertionError("module without licence was accepted")
PYTHON
if bash "$root/.github/scripts/validate-release-tag.sh" v999999.999999.999999 > "$work/tag.log" 2>&1; then
    echo 'manual release accepted a non-existent tag' >&2
    exit 1
fi
if bash "$root/.github/scripts/validate-release-tag.sh" invalid > "$work/tag.log" 2>&1; then
    echo 'manual release accepted an invalid tag' >&2
    exit 1
fi
echo 'Go module notices, lite packaging and manual tag refusals passed.'

# Exercise the local publisher end to end using fixture builds and fake external
# tools. Never use the login keychain, real signing/notary tools or GitHub.
fixture="$work/local"
mkdir -p "$fixture/.github/scripts" "$fixture/tools" "$fixture/remote"
cp "$root/.github/scripts/"{release-macos,release-pack,checksums,sha256}.sh "$fixture/.github/scripts/"
cp -R "$stage" "$fixture/template"
cat > "$fixture/.github/scripts/release-build.sh" <<'STUB'
#!/bin/bash
set -euo pipefail
name=snctl
[[ "$2" != lite ]] || name=snctl-lite
out="$RELEASE_DIR/stage/$name-darwin-$GOARCH"
[[ ! -e "$out" ]]
mkdir -p "$(dirname "$out")"
cp -R "$FAKE_ROOT/template" "$out"
printf '%s\n' "$PWD" >> "$FAKE_ROOT/build-sources"
if [[ "$PWD" == "$FAKE_ROOT" ]]; then
    [[ "$GOPROXY" == off && "$GOSUMDB" == off && "$GOVCS" == '*:off' && "$GOTOOLCHAIN" == local ]]
else
    [[ -f .tag-source && "$(dirname "$0")" == "$PWD/.github/scripts" ]]
fi
[[ "${FAKE_FAIL_BUILD:-0}" == 0 ]] || { echo 'fixture build failed' >&2; exit 1; }
printf 'build %s %s %s\n' "$GOARCH" "$2" "$CC" >> "$FAKE_LOG"
STUB
cat > "$fixture/.github/scripts/smoke-archive.sh" <<'STUB'
#!/bin/bash
set -euo pipefail
unzip -p "$1" snctl > /dev/null
printf 'smoke %s\n' "$(basename "$1")" >> "$FAKE_LOG"
STUB
cat > "$fixture/tools/fake" <<'STUB'
#!/bin/bash
set -euo pipefail
tool=${0##*/}
case "$tool" in
    uname)
        if [[ "$1" == -s ]]; then echo "${FAKE_OS:-Darwin}"; else echo arm64; fi
        exit ;;
esac
[[ "${FAKE_NO_CREDENTIALS:-0}" == 0 ]] || { echo "unexpected credential access: $tool" >&2; exit 93; }
printf '%s %s\n' "$tool" "$*" >> "$FAKE_LOG"
case "$tool" in
    git)
        [[ "$1" == -C && "$2" == "$FAKE_ROOT" ]] || exit 92
        shift 2
        case "$1 $2" in
            'fetch --quiet')
                [[ "$3" == origin && "$4" == refs/tags/*:refs/tags/* ]] || exit 92
                [[ "${FAKE_MISSING_ORIGIN_TAG:-0}" == 0 ]] || { echo 'fatal: could not find remote ref' >&2; exit 1; } ;;
            'worktree add')
                [[ "$3" == --quiet && "$4" == --detach && "$6" == refs/tags/* ]] || exit 92
                cp -R "$FAKE_ROOT/tag-source" "$5"
                printf '%s\n' "$5" > "$FAKE_ROOT/active-worktree" ;;
            'worktree remove')
                [[ "$3" == --force && "$(cat "$FAKE_ROOT/active-worktree")" == "$4" ]] || exit 92
                rm -rf "$4"
                rm "$FAKE_ROOT/active-worktree" ;;
            *) exit 92 ;;
        esac ;;
    security)
        [[ "${FAKE_NO_IDENTITY:-0}" == 0 ]] || exit 0
        echo 'Developer ID Application: Altessa Solutions Inc (SZA4GU6AL2)' ;;
    codesign) ;;
    ditto) touch "$4" ;;
    xcrun)
        [[ "$*" == 'notarytool submit '* ]] || exit 94
        echo "{\"status\":\"${FAKE_NOTARY_STATUS:-Accepted}\",\"id\":\"fixture\"}" ;;
    gh)
        case "$1 $2" in
            'release view')
                [[ "${FAKE_NO_RELEASE:-0}" == 0 ]] || exit 1
                printf '{"isDraft":%s,"isPrerelease":%s}\n' "${FAKE_DRAFT:-false}" "${FAKE_PRERELEASE:-false}" ;;
            'release download')
                [[ "${FAKE_FAIL_DOWNLOAD:-0}" == 0 ]] || exit 1
                while [[ "$1" != --dir ]]; do shift; done
                cp "$FAKE_ROOT/remote/checksums.txt" "$2/checksums.txt" ;;
            'release upload')
                [[ "$*" == *--clobber* ]] || exit 95
                while [[ "$1" != --clobber ]]; do shift; done
                shift
                for file in "$@"; do cp "$file" "$FAKE_ROOT/remote/"; done
                # Simulate failure after GitHub has accepted some/all assets.
                [[ "${FAKE_FAIL_UPLOAD:-0}" == 0 ]] || exit 1 ;;
            'workflow run')
                [[ "${FAKE_FAIL_DISPATCH:-0}" == 0 ]] || exit 1 ;;
            *) exit 96 ;;
        esac ;;
    *) exit 97 ;;
esac
STUB
# CI's minimal container need not install jq just to exercise these fixtures.
cat > "$fixture/tools/jq" <<'STUB'
#!/usr/bin/env python3
import json, sys
assert sys.argv[1] == '-r'
value = json.load(sys.stdin)[sys.argv[2].removeprefix('.')]
print(value if isinstance(value, str) else json.dumps(value))
STUB
chmod +x "$fixture/tools/fake" "$fixture/tools/jq"
for tool in uname git security codesign ditto xcrun gh; do
    ln -s fake "$fixture/tools/$tool"
done
# Model a tag with different source and helper scripts from the dirty caller.
# Git itself is faked, so the test creates no commits/tags and never fetches.
mkdir -p "$fixture/tag-source/.github"
cp -R "$fixture/.github/scripts" "$fixture/tag-source/.github/"
touch "$fixture/tag-source/.tag-source" "$fixture/untracked"
for script in release-pack smoke-archive checksums; do
    mv "$fixture/tag-source/.github/scripts/$script.sh" "$fixture/tag-source/.github/scripts/$script-impl.sh"
    cat > "$fixture/tag-source/.github/scripts/$script.sh" <<'STUB'
#!/bin/bash
set -euo pipefail
[[ -f .tag-source && "$(dirname "$0")" == "$PWD/.github/scripts" ]]
printf 'tag helper %s\n' "$(basename "$0")" >> "$FAKE_LOG"
bash "${0%.sh}-impl.sh" "$@"
STUB
done
export FAKE_ROOT="$fixture" FAKE_LOG="$fixture/calls"
assert_worktree_cleanup() {
    python3 - "$fixture" <<'PYTHON'
from pathlib import Path
import sys
root = Path(sys.argv[1])
calls = (root / 'calls').read_text().splitlines()
added = [line.split()[7] for line in calls if ' worktree add ' in line]
removed = [line.split()[6] for line in calls if ' worktree remove ' in line]
assert added == removed, (added, removed)
assert not (root / 'active-worktree').exists()
for tree in added:
    assert not Path(tree).parent.exists(), tree
PYTHON
}
local_release() {
    local result=0
    PATH="$fixture/tools:$PATH" RELEASE_DIR="$fixture/output" bash "$fixture/.github/scripts/release-macos.sh" "$@" || result=$?
    assert_worktree_cleanup || exit 1
    return "$result"
}
refuse_local() {
    local message=$1
    shift
    : > "$FAKE_LOG"
    if local_release "$@" > "$fixture/result" 2>&1; then
        echo "local publisher accepted: $message" >&2
        exit 1
    fi
    grep -qF "$message" "$fixture/result"
}
FAKE_NO_CREDENTIALS=1 refuse_local 'not a release tag' invalid
FAKE_OS=Linux FAKE_NO_CREDENTIALS=1 refuse_local 'run this on a Mac' "v$version"
RELEASE_ARCHS=arm64 FAKE_NO_CREDENTIALS=1 refuse_local 'uploads require RELEASE_ARCHS' "v$version"
FAKE_NO_RELEASE=1 refuse_local 'no release' "v$version"
FAKE_DRAFT=true refuse_local 'release is still a draft' "v$version"
FAKE_NO_IDENTITY=1 refuse_local 'no signing identity' "v$version"
FAKE_MISSING_ORIGIN_TAG=1 refuse_local 'could not fetch tag' "v$version"
if grep -qE 'worktree add|^build |^codesign |^xcrun |gh release upload' "$FAKE_LOG"; then exit 1; fi
# Cleanup must unregister the worktree and remove all temporary files on failure.
FAKE_FAIL_BUILD=1 refuse_local 'fixture build failed' "v$version"
grep -q 'worktree remove --force' "$FAKE_LOG"
if grep -qE '^codesign |^xcrun |gh release upload' "$FAKE_LOG"; then exit 1; fi
FAKE_NOTARY_STATUS=Invalid refuse_local 'notarization ended Invalid' "v$version"
if grep -q 'gh release upload' "$FAKE_LOG"; then exit 1; fi
# Dry runs use the caller's checkout and offline Go settings, with no Git calls.
: > "$FAKE_LOG"
: > "$fixture/build-sources"
FAKE_NO_CREDENTIALS=1 RELEASE_ARCHS=arm64 local_release --dry-run "v$version" > "$fixture/result"
test "$(wc -l < "$FAKE_LOG" | tr -d ' ')" == 2
test "$(sort -u "$fixture/build-sources")" == "$fixture"
test -s "$fixture/output/snctl_${version}_darwin_arm64.zip"
# Start from CI's eight checksums plus old Darwin rows and unrelated assets.
awk '$2 !~ /_darwin_/' "$work/checksums.txt" > "$fixture/non-darwin"
printf '%064d  unrelated_darwin_notes.txt\n' 2 >> "$fixture/non-darwin"
cp "$fixture/non-darwin" "$fixture/remote/checksums.txt"
# CI's release alone must not be enough to produce either Homebrew formula.
for variant in full lite; do
    flags=()
    [[ "$variant" != lite ]] || flags=(--lite)
    if bash "$root/.github/scripts/homebrew-formula.sh" ${flags[@]+"${flags[@]}"} "$version" "$fixture/non-darwin" > "$fixture/invalid" 2>/dev/null; then
        echo 'formula accepted the release before macOS upload' >&2
        exit 1
    fi
    test ! -s "$fixture/invalid"
done
printf '%064d  snctl_%s_darwin_arm64.zip\n' 3 "$version" >> "$fixture/remote/checksums.txt"
printf '%064d  *snctl-lite_%s_darwin_amd64.zip\n' 4 "$version" >> "$fixture/remote/checksums.txt"
FAKE_FAIL_DOWNLOAD=1 refuse_local '' "v$version"
if grep -q 'gh release upload' "$FAKE_LOG"; then exit 1; fi
FAKE_FAIL_UPLOAD=1 refuse_local '' "v$version"
if grep -q 'gh workflow run' "$FAKE_LOG"; then exit 1; fi
# A second attempt uses new staging and replaces the existing Darwin rows.
: > "$FAKE_LOG"
: > "$fixture/build-sources"
local_release "v$version" > "$fixture/result"
python3 - "$fixture" "$version" <<'PYTHON'
from pathlib import Path
import hashlib, sys
root, version = Path(sys.argv[1]), sys.argv[2]
lines = (root / 'remote/checksums.txt').read_text().splitlines(keepends=True)
darwin = [line for line in lines if line.split()[1].removeprefix('*').startswith(('snctl_', 'snctl-lite_')) and '_darwin_' in line.split()[1]]
assert len(darwin) == 4
assert ''.join(line for line in lines if line not in darwin) == (root / 'non-darwin').read_text()
assert len({line.split()[1] for line in lines}) == len(lines)
for line in darwin:
    digest, name = line.split()
    assert digest == hashlib.sha256((root / 'remote' / name).read_bytes()).hexdigest()
calls = (root / 'calls').read_text().splitlines()
assert calls[2] == f'git -C {root} fetch --quiet origin refs/tags/v{version}:refs/tags/v{version}'
assert 'worktree add --quiet --detach' in calls[3]
assert calls[3].endswith(f' refs/tags/v{version}')
source = calls[3].split()[7]
assert (root / 'build-sources').read_text().splitlines() == [source] * 4
assert sum(line == 'tag helper release-pack.sh' for line in calls) == 4
assert sum(line == 'tag helper smoke-archive.sh' for line in calls) == 2
assert calls.count('tag helper checksums.sh') == 1
assert sum(line.startswith('xcrun notarytool submit ') for line in calls) == 1
assert any('--keychain-profile rmctl-notary' in line for line in calls)
assert sum(line.startswith('codesign --force --options runtime --timestamp ') for line in calls) == 4
assert sum(line.startswith('codesign --verify --strict ') for line in calls) == 4
assert sum(line.startswith('smoke ') for line in calls) == 2
assert sum(line.startswith('build ') for line in calls) == 4
assert any('amd64 full clang -arch x86_64' in line for line in calls)
assert calls[-2] == f'gh workflow run homebrew.yml --repo altessa-s/snctl --ref v{version} -f tag=v{version}'
assert calls[-1] == f'git -C {root} worktree remove --force {source}'
PYTHON
FAKE_FAIL_DISPATCH=1 refuse_local '' "v$version"
local_release "v$version" > "$fixture/result"
# Both a prerelease tag and a GitHub release marked prerelease skip Homebrew.
: > "$FAKE_LOG"
local_release "v$version-rc.1" > "$fixture/result"
if grep -q 'gh workflow run' "$FAKE_LOG"; then exit 1; fi
: > "$FAKE_LOG"
FAKE_PRERELEASE=true local_release "v$version" > "$fixture/result"
if grep -q 'gh workflow run' "$FAKE_LOG"; then exit 1; fi
echo 'Local macOS publishing, tag worktrees and cleanup, offline dry runs, checksum replacement, retries and prereleases passed (fixture tools only).'

# PDFium runtime and its complete notice manifest are mandatory only on Cairo.
for os in linux windows; do
    cairo="$work/stage/snctl-$os-arm64"
    cp -R "$stage" "$cairo"
    binary=snctl
    runtime=libpdfium.so
    if [[ "$os" == windows ]]; then
        binary=snctl.exe runtime=pdfium.dll
        mv "$cairo/snctl" "$cairo/$binary"
        mkdir -p "$cairo/licenses/msys2/fixture"
        printf 'MIT fixture\n' > "$cairo/licenses/msys2/fixture/LICENSE"
    fi
    printf 'PDFium runtime fixture\n' > "$cairo/$runtime"
    mkdir -p "$cairo/licenses/pdfium/licenses"
    printf 'PDFium licence fixture\n' > "$cairo/licenses/pdfium/LICENSE"
    printf 'PDFium dependency notice fixture\n' > "$cairo/licenses/pdfium/licenses/NOTICE"
    (cd "$cairo/licenses/pdfium" && find LICENSE licenses -type f -exec bash "$sha256" {} + > notices.sha256)
    (cd "$cairo" && find licenses -type f -exec bash "$sha256" {} + > licenses.sha256)
    bash "$root/.github/scripts/release-pack.sh" "$version" full "$os" arm64 "$work" > /dev/null
    if [[ "$os" == windows ]]; then
        printf 'unlisted DLL fixture\n' > "$cairo/libjbig-0.dll"
        if bash "$root/.github/scripts/release-pack.sh" "$version" full windows arm64 "$work" > /dev/null 2>&1; then
            echo 'packer accepted a JBIG DLL' >&2
            exit 1
        fi
        rm "$cairo/libjbig-0.dll"
    fi
    for missing in "$runtime" licenses/pdfium/LICENSE licenses/pdfium/licenses/NOTICE licenses/pdfium/notices.sha256; do
        mv "$cairo/$missing" "$work/missing"
        if bash "$root/.github/scripts/release-pack.sh" "$version" full "$os" arm64 "$work" > /dev/null 2>&1; then
            echo "packer accepted missing PDFium file: $missing" >&2
            exit 1
        fi
        mv "$work/missing" "$cairo/$missing"
    done
done
echo 'PDFium runtime and complete-notice requirements passed for both Cairo targets.'
