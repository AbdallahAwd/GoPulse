#!/usr/bin/env bash
# Run a built binary against isolated state; never touch configured infrastructure.
set -euo pipefail
binary="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
expected="${2:-}"
scratch="$(mktemp -d "${TMPDIR:-/tmp}/gopulse-smoke.XXXXXX")"
pid=""
cleanup() {
  if [[ -n "$pid" ]]; then kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi
  rm -rf "$scratch"
}
trap cleanup EXIT
cd "$scratch"
export GOPULSE_DATA_DIR="$scratch/data"
export GOPULSE_SERVER_HOST=127.0.0.1
export GOPULSE_SERVER_PORT="${GOPULSE_SMOKE_PORT:-18081}"
# Override any local JWT configuration, including the legacy variable.
export GOPULSE_AUTH_JWT_SECRET=weak
export JWT_SECRET=""
info="$("$binary" --version)"
[[ "$info" == "GoPulse "* ]]
if [[ -n "$expected" ]]; then [[ "$info" == "GoPulse $expected ("* ]]; fi
if "$binary" > rejected.log 2>&1; then
  echo "Binary unexpectedly started with a weak JWT secret" >&2
  exit 1
fi
grep -q 'JWT secret must contain at least 32 characters' rejected.log

export GOPULSE_AUTH_JWT_SECRET="$(openssl rand -hex 32)"
"$binary" > server.log 2>&1 &
pid=$!
url="http://127.0.0.1:$GOPULSE_SERVER_PORT"
ready=false
for attempt in $(seq 1 30); do
  kill -0 "$pid" 2>/dev/null || { cat server.log >&2; exit 1; }
  if curl --fail --silent "$url/api/auth/has-users" > bootstrap.json; then
    ready=true
    break
  fi
  sleep 1
done
[[ "$ready" == true ]] || { cat server.log >&2; exit 1; }
jq -e '.has_users == false' bootstrap.json >/dev/null
status="$(curl --silent --output denied.json --write-out '%{http_code}' "$url/api/servers/inventory")"
[[ "$status" == 401 ]]
curl --fail --silent "$url/login" >/dev/null
echo "$info"
echo "Isolated binary smoke passed: weak-secret rejection, bootstrap, login page, unauthenticated API rejection."
