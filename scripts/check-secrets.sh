#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
scanner="${GITLEAKS:-$PWD/bin/tools/gitleaks}"
snapshot="$(mktemp -d "${TMPDIR:-/tmp}/gopulse-secrets.XXXXXX")"
trap 'rm -rf "$snapshot"' EXIT

# Include modified tracked files and new publishable files; exclude ignored local
# configuration, runtime state, binaries and private infrastructure screenshots.
while IFS= read -r -d '' path; do
  if [[ -f "$path" ]]; then
    mkdir -p "$snapshot/$(dirname "$path")"
    cp -- "$path" "$snapshot/$path"
  fi
done < <(git ls-files --cached --others --exclude-standard -z)

"$scanner" dir --redact --no-banner --max-target-megabytes 10 "$snapshot"
"$scanner" git --redact --no-banner --log-opts="--all" .
