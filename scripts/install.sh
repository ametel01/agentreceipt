#!/usr/bin/env sh
set -eu

repo="${AGENTRECEIPT_INSTALL_REPO:-ametel01/agentreceipt}"
version="${AGENTRECEIPT_INSTALL_VERSION:-latest}"
bin_dir="${AGENTRECEIPT_INSTALL_DIR:-}"
install_skill=0
requested_skill_install=0
skill_dir="${AGENTRECEIPT_SKILL_DIR:-}"
no_install_skill=0
script_dir=""
repo_skill_file=""

case "$0" in
	*/*)
		script_dir="$(CDPATH= cd "$(dirname "$0")" 2>/dev/null && pwd -P || printf '')"
		;;
esac

if [ -n "$script_dir" ] && [ -f "$script_dir/../skills/agentreceipt/SKILL.md" ]; then
	repo_skill_file="$script_dir/../skills/agentreceipt/SKILL.md"
fi

usage() {
	cat <<'USAGE'
Usage:
  curl -fsSL https://raw.githubusercontent.com/ametel01/agentreceipt/main/scripts/install.sh | sh
  curl -fsSL https://raw.githubusercontent.com/ametel01/agentreceipt/main/scripts/install.sh | sh -s -- --version v1.2.3

Options:
  --version VERSION   Install a specific release tag or SemVer. Default: latest.
  --bin-dir DIR      Install directory. Default: /usr/local/bin if writable,
                     otherwise $HOME/.local/bin.
  --install-skill    Install the AgentReceipt skill artifact non-interactively.
  --no-install-skill Skip skill installation.
  --skill-dir DIR    Install skill under DIR/agentreceipt/SKILL.md.
USAGE
}

die() {
	printf 'agentreceipt install: %s\n' "$*" >&2
	exit 1
}

print_banner() {
	cat <<'BANNER'

    _    ____ _____ _   _ _____
   / \  / ___| ____| \ | |_   _|
  / _ \| |  _|  _| |  \| | | |
 / ___ \ |_| | |___| |\  | | |
/_/   \_\____|_____|_| \_| |_|

 ____  _____ ____ _____ ___ ____ _____
|  _ \| ____/ ___| ____|_ _|  _ \_   _|
| |_) |  _|| |   |  _|  | || |_) || |
|  _ <| |__| |___| |___ | ||  __/ | |
|_| \_\_____\____|_____|___|_|    |_|
BANNER
}

print_step() {
	label="$1"
	percent="$2"
	fill="$3"
	empty="$4"
	printf '  [%s%s] %3s%%  %s\n' "$fill" "$empty" "$percent" "$label"
}

has_tty() {
	{ : </dev/tty; } 2>/dev/null && { : >/dev/tty; } 2>/dev/null
}

prompt_bool() {
	prompt="$1"
	answer=""
	printf '%s ' "$prompt" >/dev/tty
	if read -r answer </dev/tty; then
		case "$answer" in
			""|[Yy]|[Yy][Ee][Ss]) return 0 ;;
			*) return 1 ;;
		esac
	fi

	return 1
}

resolve_skill_root() {
	if [ -n "$skill_dir" ]; then
		return 0
	fi

	agents_root="${HOME}/.agents/skills"
	claude_root="${HOME}/.claude/skills"
	if [ -d "$agents_root" ]; then
		skill_dir="$agents_root"
	elif [ -d "$claude_root" ]; then
		skill_dir="$claude_root"
	else
		skill_dir="$agents_root"
	fi
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--version)
			[ "$#" -ge 2 ] || die "--version requires a value"
			version="$2"
			shift 2
			;;
		--bin-dir)
			[ "$#" -ge 2 ] || die "--bin-dir requires a value"
			bin_dir="$2"
			shift 2
			;;
		--install-skill)
			install_skill=1
			requested_skill_install=1
			shift
			;;
		--no-install-skill)
			no_install_skill=1
			shift
			;;
		--skill-dir)
			[ "$#" -ge 2 ] || die "--skill-dir requires a value"
			skill_dir="$2"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			die "unknown option: $1"
			;;
	esac
done

if [ "${AGENTRECEIPT_INSTALL_SKILL:-}" = "1" ]; then
	install_skill=1
	requested_skill_install=1
fi

if [ "$install_skill" = "1" ]; then
	requested_skill_install=1
fi

if [ "$install_skill" -ne 0 ] && [ "$no_install_skill" -ne 0 ]; then
	die "--install-skill and --no-install-skill are mutually exclusive"
fi
if [ "$no_install_skill" -ne 0 ]; then
	install_skill=0
fi
interactive_skill_prompt=0

if [ "$install_skill" -ne 0 ]; then
	requested_skill_install=1
fi

if [ "$requested_skill_install" -eq 0 ] && [ "$no_install_skill" -eq 0 ]; then
	if has_tty; then
		if prompt_bool "Install the AgentReceipt coding-agent skill? [Y/n]"; then
			install_skill=1
			interactive_skill_prompt=1
		else
			install_skill=0
		fi
	else
		printf 'No TTY detected; skipping optional AgentReceipt skill installation.\n'
	fi
fi

need() {
	command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

need curl
need tar

case "$(uname -s)" in
	Linux)
		os="linux"
		;;
	Darwin)
		os="darwin"
		;;
	*)
		die "unsupported OS: $(uname -s)"
		;;
esac

case "$(uname -m)" in
	x86_64|amd64)
		arch="amd64"
		;;
	arm64|aarch64)
		arch="arm64"
		;;
	*)
		die "unsupported architecture: $(uname -m)"
		;;
esac

if [ -z "$bin_dir" ]; then
	if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
		bin_dir="/usr/local/bin"
	else
		bin_dir="${HOME}/.local/bin"
	fi
fi

case "$version" in
	latest)
		base_url="https://github.com/${repo}/releases/latest/download"
		;;
	v*)
		base_url="https://github.com/${repo}/releases/download/${version}"
		;;
	*)
		base_url="https://github.com/${repo}/releases/download/v${version}"
		;;
esac

asset="agentreceipt_${os}_${arch}.tar.gz"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM

print_banner
print_step "resolving ${os}/${arch}" 20 "####" "................"
curl -fsSL "$base_url/$asset" -o "$tmpdir/$asset"
curl -fsSL "$base_url/SHA256SUMS" -o "$tmpdir/SHA256SUMS"
print_step "downloaded ${asset}" 45 "#########" "..........."

(
	cd "$tmpdir"
	grep "  $asset\$" SHA256SUMS > SHA256SUM
	if command -v sha256sum >/dev/null 2>&1; then
		if ! sha256sum -c SHA256SUM > "$tmpdir/checksum.log"; then
			cat "$tmpdir/checksum.log" >&2
			exit 1
		fi
	elif command -v shasum >/dev/null 2>&1; then
		if ! shasum -a 256 -c SHA256SUM > "$tmpdir/checksum.log"; then
			cat "$tmpdir/checksum.log" >&2
			exit 1
		fi
	else
		die "sha256sum or shasum is required to verify downloads"
	fi
)
print_step "verified checksum" 70 "##############" "......"

mkdir -p "$bin_dir"
print_step "extracting archive" 85 "#################" "..."
tar -C "$tmpdir" -xzf "$tmpdir/$asset" agentreceipt
install -m 0755 "$tmpdir/agentreceipt" "$bin_dir/agentreceipt"
print_step "installed binary" 100 "####################" ""

if [ "$install_skill" -ne 0 ]; then
	skill_archive_file="$tmpdir/agentreceipt-skill/SKILL.md"
	if tar -C "$tmpdir" -xzf "$tmpdir/$asset" agentreceipt-skill/SKILL.md 2>/dev/null && [ -f "$skill_archive_file" ]; then
		skill_source_file="$skill_archive_file"
	elif [ -n "$repo_skill_file" ]; then
		skill_source_file="$repo_skill_file"
	else
		die "release archive for ${base_url} is missing agentreceipt-skill/SKILL.md; use a release that includes the skill artifact or run scripts/install.sh from a repo checkout"
	fi
	resolve_skill_root
	target_skill_dir="${skill_dir%/}/agentreceipt"
	target_skill_file="$target_skill_dir/SKILL.md"
	mkdir -p "$target_skill_dir"
	if [ -f "$target_skill_file" ] && ! cmp -s "$skill_source_file" "$target_skill_file"; then
		if [ "$interactive_skill_prompt" -eq 1 ] && has_tty; then
			if prompt_bool "Target skill already exists at ${target_skill_file}; overwrite? [y/N]"; then
				cp "$skill_source_file" "$target_skill_file"
			else
				printf 'Keeping existing AgentReceipt skill at %s\n' "$target_skill_file"
			fi
		else
			die "existing skill at ${target_skill_file} differs; remove it first for noninteractive installs"
		fi
	else
		cp "$skill_source_file" "$target_skill_file"
	fi
fi

printf '\nInstalled agentreceipt to %s/agentreceipt\n' "$bin_dir"
if [ "$install_skill" -ne 0 ]; then
	printf 'Installed AgentReceipt coding-agent skill to %s\n' "$target_skill_file"
fi
printf 'Run: agentreceipt version\n'
