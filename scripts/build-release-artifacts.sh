#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"

usage() {
	cat <<'USAGE'
Usage:
  scripts/build-release-artifacts.sh VERSION [DIST_DIR]

Build release archives for GitHub Releases. VERSION may be passed with or
without a leading "v".

Environment:
  AGENTRECEIPT_RELEASE_TARGETS  Space-separated GOOS/GOARCH targets.
USAGE
}

die() {
	printf 'build-release-artifacts: %s\n' "$*" >&2
	exit 1
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
	usage
	exit 0
fi

version="${1:-}"
dist="${2:-dist}"
[[ -n "$version" ]] || die "version is required"
version="${version#v}"

if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]]; then
	die "version must be a semantic version, with optional leading v: $version"
fi

targets="${AGENTRECEIPT_RELEASE_TARGETS:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64}"
ldflags="-s -w -X github.com/ametel01/agentreceipt/internal/buildinfo.version=$version"

rm -rf "$dist"
mkdir -p "$dist"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

for target in $targets; do
	goos="${target%/*}"
	goarch="${target#*/}"
	[[ -n "$goos" && -n "$goarch" && "$goos" != "$goarch" ]] || die "invalid target: $target"

	package_dir="$tmpdir/agentreceipt_${goos}_${goarch}"
	rm -rf "$package_dir"
	mkdir -p "$package_dir"

	CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$ldflags" -o "$package_dir/agentreceipt" .
	mkdir -p "$package_dir/agentreceipt-skill"
	cp "$repo_root/skills/agentreceipt/SKILL.md" "$package_dir/agentreceipt-skill/SKILL.md"
	tar -C "$package_dir" -czf "$dist/agentreceipt_${goos}_${goarch}.tar.gz" agentreceipt agentreceipt-skill/SKILL.md
done

(
	cd "$dist"
	shasum -a 256 *.tar.gz > SHA256SUMS
)
