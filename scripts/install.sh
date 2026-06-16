#!/usr/bin/env sh
set -eu

repo="${AGENTRECEIPT_INSTALL_REPO:-ametel01/agentreceipt}"
version="${AGENTRECEIPT_INSTALL_VERSION:-latest}"
bin_dir="${AGENTRECEIPT_INSTALL_DIR:-}"

usage() {
	cat <<'USAGE'
Usage:
  curl -fsSL https://raw.githubusercontent.com/ametel01/agentreceipt/main/scripts/install.sh | sh
  curl -fsSL https://raw.githubusercontent.com/ametel01/agentreceipt/main/scripts/install.sh | sh -s -- --version v1.2.3

Options:
  --version VERSION   Install a specific release tag or SemVer. Default: latest.
  --bin-dir DIR      Install directory. Default: /usr/local/bin if writable,
                     otherwise $HOME/.local/bin.
USAGE
}

die() {
	printf 'agentreceipt install: %s\n' "$*" >&2
	exit 1
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
		-h|--help)
			usage
			exit 0
			;;
		*)
			die "unknown option: $1"
			;;
	esac
done

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

curl -fsSL "$base_url/$asset" -o "$tmpdir/$asset"
curl -fsSL "$base_url/SHA256SUMS" -o "$tmpdir/SHA256SUMS"

(
	cd "$tmpdir"
	grep "  $asset\$" SHA256SUMS > SHA256SUM
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum -c SHA256SUM
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 -c SHA256SUM
	else
		die "sha256sum or shasum is required to verify downloads"
	fi
)

mkdir -p "$bin_dir"
tar -C "$tmpdir" -xzf "$tmpdir/$asset" agentreceipt
install -m 0755 "$tmpdir/agentreceipt" "$bin_dir/agentreceipt"

printf 'Installed agentreceipt to %s/agentreceipt\n' "$bin_dir"
printf 'Run: agentreceipt version\n'
