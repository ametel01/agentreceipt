#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"

sh -n "$script_dir/install.sh"
sh -n "$script_dir/extract-release-notes.sh"
bash -n "$script_dir/build-release-artifacts.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

(
	cd "$repo_root"
	AGENTRECEIPT_RELEASE_TARGETS="linux/amd64" "$script_dir/build-release-artifacts.sh" 1.2.3 "$tmpdir/dist"
)

test -s "$tmpdir/dist/agentreceipt_linux_amd64.tar.gz"
test -s "$tmpdir/dist/SHA256SUMS"
grep -q "  agentreceipt_linux_amd64.tar.gz$" "$tmpdir/dist/SHA256SUMS"
tar -tzf "$tmpdir/dist/agentreceipt_linux_amd64.tar.gz" | grep -qx "agentreceipt"

(
	cd "$tmpdir/dist"
	shasum -a 256 -c SHA256SUMS
)
