#!/usr/bin/env bash
# Exercise the real scanner in a disposable repository; no real credentials used.
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
scanner="${GITLEAKS:-$root/bin/tools/gitleaks}"
fixture="$(mktemp -d "${TMPDIR:-/tmp}/gopulse-scanner-test.XXXXXX")"
trap 'rm -rf "$fixture"' EXIT
cp "$root/.gitleaks.toml" "$fixture/"
mkdir -p "$fixture/scripts" "$fixture/docs" "$fixture/internal/handlers"
cp "$root/scripts/check-secrets.sh" "$fixture/scripts/"
cd "$fixture"
git init -q
git config user.name "Scanner test"
git config user.email "scanner@example.invalid"
printf 'private.env\n' > .gitignore
# Construct a deliberately synthetic, high-entropy test credential at runtime.
token="$(openssl rand -hex 32)"
printf 'api_key = "%s"\n' "$token" > private.env
placeholder="YOUR_FUNCTION_""TOKEN"
fixture_fragment="0123456789abcdef"
printf "curl https://example.invalid --header 'Authorization: Bearer %s'\n" "$placeholder" > docs/functions.md
printf 'const testAuthSecret = "%s%s"\n' "$fixture_fragment" "$fixture_fragment" > internal/handlers/mcp_handler_test.go
git add .
git commit -qm "Clean fixtures"

scan() { GITLEAKS="$scanner" bash scripts/check-secrets.sh "$@" > scan.log 2>&1; }
expect_failure() {
  local result=0
  scan "$@" || result=$?
  if [[ "$result" != 1 ]]; then echo "Expected $* to report findings (exit 1), got $result" >&2; exit 1; fi
  if grep -q -F "$token" scan.log; then echo "Scanner leaked a value" >&2; exit 1; fi
}
# Ignore the captured scanner output itself, without weakening the scanner config.
printf 'scan.log\n' >> .gitignore
scan all # Exact placeholders and ignored local credentials are allowed.
printf 'api_key = "%s"\n' "$token" > docs/functions.md
expect_failure source # Same documentation path must still catch real-looking keys.
git add docs/functions.md
git commit -qm "Synthetic historical finding"
printf 'Read the function guide.\n' > docs/functions.md
git add docs/functions.md
git commit -qm "Remove synthetic finding"
scan source # Current source is clean.
expect_failure history # Removal must not conceal history.
printf 'api_key = "%s"\n' "$token" > new-file.conf
expect_failure all # Both scopes run, even when the first scan fails.
grep -q 'Scanning all locally available Git' scan.log
rm new-file.conf
# The exact placeholder must not be allowlisted in unrelated source files.
printf "curl https://example.invalid --header 'Authorization: Bearer %s'\n" "$placeholder" > unrelated.sh
expect_failure source
echo "Secret-scanner regression checks passed."
