#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

version="${1:-dev}"
arch="${2:-amd64}"
if [[ "$version" != dev && ! "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta|rc)\.[1-9][0-9]*)?$ ]]; then
  echo "Version must be dev, vMAJOR.MINOR.PATCH, or vMAJOR.MINOR.PATCH-{alpha,beta,rc}.N" >&2
  exit 2
fi
case "$arch" in amd64|arm64) ;; *) echo "Unsupported Linux architecture: $arch" >&2; exit 2 ;; esac

name="gopulse_${version}_linux_${arch}"
destination="$PWD/dist"
stage="$(mktemp -d "${TMPDIR:-/tmp}/gopulse-release.XXXXXX")"
trap 'rm -rf "$stage"' EXIT
mkdir -p "$destination" "$stage/$name"
commit="$(git rev-parse HEAD)"
if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  commit="$commit-dirty"
fi
built_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
  -ldflags="-s -w -X main.version=$version -X main.commit=$commit -X main.builtAt=$built_at" \
  -o "$stage/$name/gopulse" ./cmd/server

# Install tools for the build host; collect licenses for the target platform.
mkdir -p bin/tools
GOBIN="$PWD/bin/tools" go install github.com/google/go-licenses/v2@v2.0.1
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOROOT="$(go env GOROOT)" \
  bin/tools/go-licenses save ./cmd/server --save_path="$stage/$name/third_party_licenses" \
  --ignore=github.com/AbdallahAwd/GoPulse
cp LICENSE README.md SECURITY.md CHANGELOG.md THIRD_PARTY_NOTICES.md config.example.yaml .env.example "$stage/$name/"
cp "$(go env GOROOT)/LICENSE" "$stage/$name/GO_LICENSE"

# Copy only tracked documentation, never ignored private screenshots or local state.
while IFS= read -r -d '' path; do
  [[ -f "$path" ]] || continue
  mkdir -p "$stage/$name/$(dirname "$path")"
  cp -- "$path" "$stage/$name/$path"
done < <(git ls-files --cached --others --exclude-standard -z -- docs)

tar -czf "$destination/$name.tar.gz" -C "$stage" "$name"
(
  cd "$destination"
  if command -v sha256sum >/dev/null; then
    sha256sum "$name.tar.gz" > "$name.tar.gz.sha256"
  else
    shasum -a 256 "$name.tar.gz" > "$name.tar.gz.sha256"
  fi
)
echo "Packaged dist/$name.tar.gz (Linux/$arch, $commit)"
