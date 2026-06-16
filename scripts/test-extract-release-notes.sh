#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$script_dir/extract-release-notes.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

changelog="$tmpdir/CHANGELOG.md"
cat > "$changelog" <<'CHANGELOG'
# Changelog

## [Unreleased]

### Added

- Future change.

## [1.2.3] - 2026-06-17

### Added

- Added release artifact signing.

### Fixed

- Fixed release archive checksums.

## [1.2.2] - 2026-06-10

### Changed

- Previous release note.

## v1.2.1 - 2026-06-03

### Removed

- Legacy release note.

## [1.2.0] - 2026-05-27
CHANGELOG

expected_123="$(cat <<'EXPECTED'
### Added

- Added release artifact signing.

### Fixed

- Fixed release archive checksums.
EXPECTED
)"

expected_unreleased="$(cat <<'EXPECTED'
### Added

- Future change.
EXPECTED
)"

notes_unreleased="$("$script" --unreleased "$changelog")"
[[ "$notes_unreleased" == "$expected_unreleased" ]]

notes_unreleased_flag="$("$script" --version Unreleased --changelog "$changelog")"
[[ "$notes_unreleased_flag" == "$expected_unreleased" ]]

notes_unreleased_flag_positional="$("$script" --version Unreleased "$changelog")"
[[ "$notes_unreleased_flag_positional" == "$expected_unreleased" ]]

notes_123="$("$script" 1.2.3 "$changelog")"
[[ "$notes_123" == "$expected_123" ]]

notes_v123="$("$script" --version v1.2.3 --changelog "$changelog")"
[[ "$notes_v123" == "$expected_123" ]]

notes_121="$("$script" 1.2.1 "$changelog")"
[[ "$notes_121" == *"### Removed"* ]]
[[ "$notes_121" == *"Legacy release note."* ]]

if "$script" 9.9.9 "$changelog" > "$tmpdir/missing.out" 2> "$tmpdir/missing.err"; then
	printf 'expected missing version to fail\n' >&2
	exit 1
fi
grep -q "were not found" "$tmpdir/missing.err"

if "$script" 1.2.0 "$changelog" > "$tmpdir/empty.out" 2> "$tmpdir/empty.err"; then
	printf 'expected empty section to fail\n' >&2
	exit 1
fi
grep -q "are empty" "$tmpdir/empty.err"

if "$script" not-semver "$changelog" > "$tmpdir/bad-version.out" 2> "$tmpdir/bad-version.err"; then
	printf 'expected invalid semver to fail\n' >&2
	exit 1
fi
grep -q "semantic version" "$tmpdir/bad-version.err"
