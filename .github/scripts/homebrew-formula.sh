#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Write a Homebrew formula for a release to stdout.
#
# Usage: homebrew-formula.sh [--lite] VERSION CHECKSUMS
#
# VERSION is the release version without the leading "v"; CHECKSUMS is the
# release's checksums.txt. The formula installs the prebuilt macOS and Linux
# archives of that release; a missing checksum fails the script before any of
# the formula is written.
#
# Without --lite it is snctl.rb, class Snctl: the full build. Linux links
# the system Cairo stack, so the formula depends on those libraries.
#
# With --lite it is snctl-lite.rb, class SnctlLite: the snctl-lite archives,
# the build without the renderer, whose binary needs no library beyond the
# system's and goes straight into bin. Both binaries are named snctl, so each
# formula conflicts with the other.
#
# Both install the completion scripts of bash, zsh and fish, which the
# installed binary writes with "snctl completion <shell>", and define the
# mirror service, so "brew services start snctl" keeps every mirror of
# config.toml in step: it runs "snctl mirror run --service", which writes its
# own log; brew's log file catches what it writes before that log opens.
set -euo pipefail

lite=0
if [[ "${1:-}" == --lite ]]; then
    lite=1
    shift
fi
version=${1:?version}
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo 'invalid version' >&2; exit 2; }
checksums=${2:?checksums file}
base="https://github.com/altessa-s/snctl/releases/download/v${version}"
if ((lite)); then
    name=snctl-lite class=SnctlLite other=snctl
    desc="Command-line client for the Supernote cloud, without rendering"
else
    name=snctl class=Snctl other=snctl-lite
    desc="Command-line client for the Supernote cloud"
fi

# sum SUFFIX: the checksum of the archive ${name}_${version}_SUFFIX.
sum() {
    local file="${name}_${version}_$1"
    local value
    value=$(awk -v f="$file" '$2 == f || $2 == "*" f { print $1 }' "$checksums")
    [[ "$value" =~ ^[0-9a-f]{64}$ ]] || { printf 'no checksum for %s\n' "$file" >&2; exit 1; }
    printf '%s' "$value"
}

darwin_arm64=$(sum darwin_arm64.zip)
darwin_amd64=$(sum darwin_amd64.zip)
linux_arm64=$(sum linux_arm64.tar.gz)
linux_amd64=$(sum linux_amd64.tar.gz)

linux_deps=""
install='    bin.install "snctl"'
if ((!lite)); then
    linux_deps='    depends_on "cairo"
    depends_on "gdk-pixbuf"
    depends_on "freetype"

'

    install='    if OS.linux?
      libexec.install "snctl", "libpdfium.so"
      bin.install_symlink libexec/"snctl"
    else
      bin.install "snctl"
    end'
fi

cat <<EOF
# This file is written by the snctl release workflow; edits are overwritten.
class ${class} < Formula
  desc "${desc}"
  homepage "https://github.com/altessa-s/snctl"
  version "${version}"
  license "MIT"

  on_macos do
    on_arm do
      url "${base}/${name}_${version}_darwin_arm64.zip"
      sha256 "${darwin_arm64}"
    end
    on_intel do
      url "${base}/${name}_${version}_darwin_amd64.zip"
      sha256 "${darwin_amd64}"
    end
  end

  on_linux do
${linux_deps}    on_arm do
      url "${base}/${name}_${version}_linux_arm64.tar.gz"
      sha256 "${linux_arm64}"
    end
    on_intel do
      url "${base}/${name}_${version}_linux_amd64.tar.gz"
      sha256 "${linux_amd64}"
    end
  end

  conflicts_with "${other}", because: "both install an snctl binary"

  def install
${install}
    generate_completions_from_executable(bin/"snctl", "completion")
    doc.install "licenses" if File.directory?("licenses")
  end

  service do
    run [opt_bin/"snctl", "mirror", "run", "--service"]
    keep_alive successful_exit: false
    log_path var/"log/snctl-mirror.log"
    error_log_path var/"log/snctl-mirror.log"
  end

  test do
    assert_equal version.to_s, shell_output("#{bin}/snctl version").strip
  end
end
EOF
