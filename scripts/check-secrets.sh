#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
scanner="${GITLEAKS:-$PWD/bin/tools/gitleaks}"
mode="${1:-all}"
case "$mode" in
  all|source|history) ;;
  *) echo "Usage: $0 [all|source|history]" >&2; exit 2 ;;
esac
snapshot="$(mktemp -d "${TMPDIR:-/tmp}/gopulse-secrets.XXXXXX")"
trap 'rm -rf "$snapshot"' EXIT

status=0
scan() {
  local result=0
  # --verbose identifies the file, rule and line; --redact keeps values out of CI.
  "$scanner" "$@" --config "$PWD/.gitleaks.toml" --redact --verbose --no-banner --no-color || result=$?
  if [[ "$result" -ne 0 ]]; then status="$result"; fi
}

# Include modified tracked files and new publishable files; exclude ignored local
# configuration, runtime state, binaries and private infrastructure screenshots.
if [[ "$mode" != history ]]; then
  while IFS= read -r -d '' path; do
    if [[ -f "$path" ]]; then
      mkdir -p "$snapshot/$(dirname "$path")"
      cp -- "$path" "$snapshot/$path"
    fi
  done < <(git ls-files --cached --others --exclude-standard -z)
  echo "Scanning publishable source (including uncommitted changes)..."
  scan dir --max-target-megabytes 10 "$snapshot"
fi

if [[ "$mode" != source ]]; then
  echo "Scanning all locally available Git branches and tags..."
  scan git --log-opts="--all" .
fi

if [[ "$status" -ne 0 ]]; then
  echo "Secret scan failed. Review the redacted findings above and SECURITY.md." >&2
  echo "Removing a value from today's source does not remove it from Git history." >&2
fi
exit "$status"
