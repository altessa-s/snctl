#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license in the LICENSE file.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$root"
ruby -ryaml -e '
  steps = YAML.load_file(".github/workflows/bench.yml").fetch("jobs").fetch("bench").fetch("steps")
  abort "benchmark steps must run unconditionally" if steps.any? { |s| s.key?("if") }
  download = steps.find { |s| s["run"] == "go mod download" }
  abort "expected a plain Go module download" unless download && !download.key?("env")
  abort "missing benchmark command" unless steps.any? { |s| s["run"].to_s.start_with?("go test ") && s["run"].include?("-bench ") }
'
# Compile the benchmark packages with an empty home and public Go settings.
# Keep the runner's caches so CI can reuse its verified downloads and builds.
env -i PATH="$PATH" HOME="$work" GOENV=off GOAUTH=off GIT_TERMINAL_PROMPT=0 \
    GOMODCACHE="$(go env GOMODCACHE)" GOCACHE="$(go env GOCACHE)" CC="$(go env CC)" \
    CGO_CFLAGS="$(go env CGO_CFLAGS)" CGO_LDFLAGS="$(go env CGO_LDFLAGS)" \
    bash -ec 'go mod download && go test -buildvcs=false -run "^$" ./...'
echo 'Benchmark packages build without credentials.'
