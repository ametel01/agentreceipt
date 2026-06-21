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
archive_listing="$tmpdir/archive-listing.txt"

(
	cd "$repo_root"
	AGENTRECEIPT_RELEASE_TARGETS="linux/amd64" "$script_dir/build-release-artifacts.sh" "$release_version" "$tmpdir/dist"
)

test -s "$archive_path"
test -s "$tmpdir/dist/SHA256SUMS"
grep -q "  agentreceipt_linux_amd64.tar.gz$" "$tmpdir/dist/SHA256SUMS"
tar -tzf "$archive_path" > "$archive_listing"
grep -qx "agentreceipt$" "$archive_listing"
grep -qx "agentreceipt-skill/SKILL.md$" "$archive_listing"
mkdir -p "$extract_dir"
tar -xzf "$archive_path" -C "$extract_dir"
cmp -s "$expected_skill" "$extract_dir/agentreceipt-skill/SKILL.md"

(
	cd "$tmpdir/dist"
	shasum -a 256 -c SHA256SUMS
)

fake_dist="$tmpdir/fake-dist"
fake_home="$tmpdir/fake-home"
fake_bin="$fake_home/bin"
mkdir -p "$fake_dist" "$fake_bin"
cp "$tmpdir/dist/agentreceipt_linux_amd64.tar.gz" "$fake_dist/"
cp "$tmpdir/dist/SHA256SUMS" "$fake_dist/"
export FAKE_DIST="$fake_dist"

cat > "$fake_bin/curl" <<'EOF'
#!/usr/bin/env sh
set -eu

while [ "$#" -gt 0 ]; do
	case "$1" in
		-o)
			out="$2"
			shift 2
			;;
		http*://*/*)
			url="$1"
			shift
			;;
		*)
			shift
			;;
	esac
done

file="${url##*/}"
if [ -z "${file:-}" ] || [ -z "${out:-}" ]; then
	exit 1
fi

src="$FAKE_DIST/$file"
test -f "$src" || exit 1
cp "$src" "$out"
EOF
chmod +x "$fake_bin/curl"
cat > "$fake_bin/uname" <<'EOF'
#!/usr/bin/env sh
if [ "$1" = "-s" ]; then
  echo "Linux"
  exit 0
fi

if [ "$1" = "-m" ]; then
  echo "x86_64"
  exit 0
fi

command uname "$@"
EOF
chmod +x "$fake_bin/uname"

# noninteractive install requested fixture
install_home="$tmpdir/installer-install"
mkdir -p "$install_home"
PATH="$fake_bin:$PATH" HOME="$install_home" AGENTRECEIPT_INSTALL_VERSION="1.2.3" sh "$script_dir/install.sh" --bin-dir "$install_home/bin" --install-skill --skill-dir "$install_home/skills"
test -x "$install_home/bin/agentreceipt"
cmp -s "$expected_skill" "$install_home/skills/agentreceipt/SKILL.md"

# non-install fixture
no_skill_home="$tmpdir/installer-no-skill"
mkdir -p "$no_skill_home"
PATH="$fake_bin:$PATH" HOME="$no_skill_home" AGENTRECEIPT_INSTALL_VERSION="1.2.3" sh "$script_dir/install.sh" --bin-dir "$no_skill_home/bin" --no-install-skill
test -x "$no_skill_home/bin/agentreceipt"
if [ -d "$no_skill_home/skills" ]; then
	exit 1
fi

# no tty default behavior skips optional skill installation
no_tty_home="$tmpdir/installer-no-tty"
mkdir -p "$no_tty_home"
no_tty_log="$tmpdir/no-tty-install.log"
PATH="$fake_bin:$PATH" HOME="$no_tty_home" AGENTRECEIPT_INSTALL_VERSION="1.2.3" sh "$script_dir/install.sh" --bin-dir "$no_tty_home/bin" > "$no_tty_log"
test -x "$no_tty_home/bin/agentreceipt"
test ! -d "$no_tty_home/skills"
grep -q "No TTY detected; skipping optional AgentReceipt skill installation." "$no_tty_log"

# env-driven install fixture
env_home="$tmpdir/installer-env"
mkdir -p "$env_home"
PATH="$fake_bin:$PATH" HOME="$env_home" AGENTRECEIPT_INSTALL_VERSION="1.2.3" AGENTRECEIPT_INSTALL_SKILL="1" AGENTRECEIPT_SKILL_DIR="$env_home/custom-skills" sh "$script_dir/install.sh" --bin-dir "$env_home/bin"
cmp -s "$expected_skill" "$env_home/custom-skills/agentreceipt/SKILL.md"

# local checkout fallback fixture for older archives without agentreceipt-skill/SKILL.md
old_dist="$tmpdir/old-dist"
old_package="$tmpdir/old-package"
mkdir -p "$old_dist" "$old_package"
cp "$extract_dir/agentreceipt" "$old_package/agentreceipt"
tar -C "$old_package" -czf "$old_dist/agentreceipt_linux_amd64.tar.gz" agentreceipt
(
	cd "$old_dist"
	shasum -a 256 *.tar.gz > SHA256SUMS
)
old_archive_home="$tmpdir/installer-old-archive"
mkdir -p "$old_archive_home"
FAKE_DIST="$old_dist" PATH="$fake_bin:$PATH" HOME="$old_archive_home" AGENTRECEIPT_INSTALL_VERSION="1.2.3" AGENTRECEIPT_INSTALL_SKILL="1" AGENTRECEIPT_SKILL_DIR="$old_archive_home/skills" sh "$script_dir/install.sh" --bin-dir "$old_archive_home/bin"
test -x "$old_archive_home/bin/agentreceipt"
cmp -s "$expected_skill" "$old_archive_home/skills/agentreceipt/SKILL.md"

# identical skill reuse fixture
identical_home="$tmpdir/installer-identical"
mkdir -p "$identical_home/skills/agentreceipt"
cp "$expected_skill" "$identical_home/skills/agentreceipt/SKILL.md"
PATH="$fake_bin:$PATH" HOME="$identical_home" AGENTRECEIPT_INSTALL_VERSION="1.2.3" AGENTRECEIPT_INSTALL_SKILL="1" AGENTRECEIPT_SKILL_DIR="$identical_home/skills" sh "$script_dir/install.sh" --bin-dir "$identical_home/bin"
cmp -s "$expected_skill" "$identical_home/skills/agentreceipt/SKILL.md"

# divergent skill failure fixture
divergent_home="$tmpdir/installer-divergent"
mkdir -p "$divergent_home/skills/agentreceipt"
cat <<'EOF' > "$divergent_home/skills/agentreceipt/SKILL.md"
This content differs from the release skill.
EOF
if PATH="$fake_bin:$PATH" HOME="$divergent_home" AGENTRECEIPT_INSTALL_VERSION="1.2.3" AGENTRECEIPT_INSTALL_SKILL="1" AGENTRECEIPT_SKILL_DIR="$divergent_home/skills" sh "$script_dir/install.sh" --bin-dir "$divergent_home/bin"; then
	exit 1
fi
