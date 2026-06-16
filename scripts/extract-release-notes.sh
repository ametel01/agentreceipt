#!/usr/bin/env sh
set -eu

usage() {
	cat <<'USAGE'
Usage:
  scripts/extract-release-notes.sh VERSION [CHANGELOG]
  scripts/extract-release-notes.sh --version VERSION [--changelog CHANGELOG]
  scripts/extract-release-notes.sh --unreleased [CHANGELOG]

Extract the body of the CHANGELOG.md section for VERSION.
VERSION may be passed with or without a leading "v", or as "Unreleased".
USAGE
}

die() {
	printf 'extract-release-notes: %s\n' "$*" >&2
	exit 1
}

version=""
changelog="CHANGELOG.md"
positional=0
version_from_option=0

while [ "$#" -gt 0 ]; do
	case "$1" in
		--version)
			[ "$#" -ge 2 ] || die "--version requires a value"
			version="$2"
			version_from_option=1
			shift 2
			;;
		--changelog)
			[ "$#" -ge 2 ] || die "--changelog requires a value"
			changelog="$2"
			shift 2
			;;
		--unreleased)
			[ -z "$version" ] || die "version provided more than once"
			version="Unreleased"
			version_from_option=1
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		--)
			shift
			break
			;;
		-*)
			die "unknown option: $1"
			;;
		*)
			positional=$((positional + 1))
			if [ "$version_from_option" -eq 1 ]; then
				case "$positional" in
					1)
						changelog="$1"
						;;
					*)
						die "too many positional arguments"
						;;
				esac
			else
				case "$positional" in
					1)
						version="$1"
						;;
					2)
						changelog="$1"
						;;
					*)
						die "too many positional arguments"
						;;
				esac
			fi
			shift
			;;
	esac
done

[ "$#" -eq 0 ] || die "too many positional arguments"
[ -n "$version" ] || die "version is required"
[ -f "$changelog" ] || die "changelog not found: $changelog"

normalized_version="${version#v}"
if [ "$normalized_version" = "unreleased" ] || [ "$normalized_version" = "Unreleased" ]; then
	normalized_version="Unreleased"
elif ! printf '%s\n' "$normalized_version" | grep -Eq '^[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*(-[0-9A-Za-z.-][0-9A-Za-z.-]*)?(\+[0-9A-Za-z.-][0-9A-Za-z.-]*)?$'; then
	die "version must be a semantic version, with optional leading v, or Unreleased: $version"
fi

tmp="${TMPDIR:-/tmp}/agentreceipt-release-notes.$$"
trap 'rm -f "$tmp"' EXIT HUP INT TERM

set +e
awk -v target="$normalized_version" '
function trim(value) {
	gsub(/^[[:space:]]+/, "", value)
	gsub(/[[:space:]]+$/, "", value)
	return value
}

function heading_version(line, heading, close_pos, parts) {
	heading = line
	sub(/^##[[:space:]]+/, "", heading)
	heading = trim(heading)
	if (heading ~ /^\[[^]]+\]/) {
		close_pos = index(heading, "]")
		heading = substr(heading, 2, close_pos - 2)
	} else {
		split(heading, parts, /[[:space:]]+/)
		heading = parts[1]
	}
	sub(/^v/, "", heading)
	return heading
}

function is_h2(line) {
	return line ~ /^##[[:space:]]+/ && line !~ /^###[[:space:]]+/
}

BEGIN {
	found = 0
	started = 0
	count = 0
}

is_h2($0) {
	if (found) {
		exit
	}
	if (heading_version($0) == target) {
		found = 1
		next
	}
}

found {
	if (!started && $0 ~ /^[[:space:]]*$/) {
		next
	}
	started = 1
	lines[++count] = $0
}

END {
	if (!found) {
		exit 1
	}
	while (count > 0 && lines[count] ~ /^[[:space:]]*$/) {
		count--
	}
	if (count == 0) {
		exit 3
	}
	for (i = 1; i <= count; i++) {
		print lines[i]
	}
}
' "$changelog" > "$tmp"
status="$?"
set -e

case "$status" in
	0)
		cat "$tmp"
		;;
	1)
		die "release notes for version $version were not found in $changelog"
		;;
	3)
		die "release notes for version $version are empty in $changelog"
		;;
	*)
		die "failed to parse $changelog"
		;;
esac
