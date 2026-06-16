#!/usr/bin/env bash
set -euo pipefail

packages="$(go list ./...)"
test -n "$packages"
go test ./...

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

go build -o "$tmpdir/agentreceipt" .
help_output="$("$tmpdir/agentreceipt" --help)"
version_output="$("$tmpdir/agentreceipt" version)"
claude_output="$("$tmpdir/agentreceipt" install claude)"

[[ "$help_output" == *"install codex"* ]]
[[ "$version_output" == *"agentreceipt"* ]]
[[ "$claude_output" == *"deferred in the Codex-first MVP"* ]]
