#!/bin/bash
# Exercise the shared validator without making commits or changing git config.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
for message in 'feat(profile): add local endpoint profiles' 'fix(secrets): preserve tokens on failed migration' 'build(deps): update dependencies' 'docs: describe the skeleton'; do
    printf '%s\n' "$message" | bash "$root/.github/scripts/validate-commit-message.sh"
done
for message in 'feat(root): add profiles' 'feat(unknown): add profiles' 'not conventional' 'feat(cli): Add profiles'; do
    if printf '%s\n' "$message" | bash "$root/.github/scripts/validate-commit-message.sh" >/dev/null 2>&1; then
        echo "accepted invalid message: $message" >&2
        exit 1
    fi
done
echo 'Commit message validation passed'
