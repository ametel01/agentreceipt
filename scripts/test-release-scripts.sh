#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"

sh -n "$script_dir/install.sh"
sh -n "$script_dir/extract-release-notes.sh"
bash -n "$script_dir/build-release-artifacts.sh"

skill_source="$repo_root/skills/agentreceipt/SKILL.md"
test -s "$skill_source"
test "$(head -n 1 "$skill_source")" = "---"
grep -q "^name: agentreceipt$" "$skill_source"
grep -q "^description: " "$skill_source"
grep -q "agentreceipt replay --session" "$skill_source"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
release_version="1.2.3"
archive_path="$tmpdir/dist/agentreceipt_linux_amd64.tar.gz"
extract_dir="$tmpdir/extracted"
expected_skill="$repo_root/skills/agentreceipt/SKILL.md"

(
	cd "$repo_root"
	AGENTRECEIPT_RELEASE_TARGETS="linux/amd64" "$script_dir/build-release-artifacts.sh" "$release_version" "$tmpdir/dist"
)

test -s "$archive_path"
test -s "$tmpdir/dist/SHA256SUMS"
grep -q "  agentreceipt_linux_amd64.tar.gz$" "$tmpdir/dist/SHA256SUMS"
tar -tzf "$archive_path" | grep -qx "agentreceipt$"
tar -tzf "$archive_path" | grep -qx "agentreceipt-skill/SKILL.md$"
mkdir -p "$extract_dir"
tar -xzf "$archive_path" -C "$extract_dir"
cmp -s "$expected_skill" "$extract_dir/agentreceipt-skill/SKILL.md"

(
	cd "$tmpdir/dist"
	shasum -a 256 -c SHA256SUMS
)
