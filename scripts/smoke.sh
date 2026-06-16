#!/usr/bin/env bash
set -euo pipefail

packages="$(go list ./...)"
test -n "$packages"
go test ./...
./scripts/test-extract-release-notes.sh

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

go build -o "$tmpdir/agentreceipt" .
help_output="$("$tmpdir/agentreceipt" --help)"
version_output="$("$tmpdir/agentreceipt" version)"
claude_output="$("$tmpdir/agentreceipt" install claude)"

[[ "$help_output" == *"install codex"* ]]
[[ "$version_output" == *"agentreceipt"* ]]
[[ "$claude_output" == *"deferred in the Codex-first MVP"* ]]

repo="$tmpdir/repo"
keydir="$tmpdir/keys"
home="$tmpdir/home"
mkdir -p "$repo"
git -C "$repo" init >/dev/null
git -C "$repo" config user.email agentreceipt@example.test
git -C "$repo" config user.name "AgentReceipt Smoke"
printf 'hello\n' > "$repo/README.md"
git -C "$repo" add README.md
git -C "$repo" commit -m initial >/dev/null

export AGENTRECEIPT_KEY_DIR="$keydir"
export AGENTRECEIPT_HOME="$home"

init_output="$("$tmpdir/agentreceipt" --repo "$repo" init)"
start_output="$("$tmpdir/agentreceipt" --repo "$repo" start)"
session_id="${start_output##* }"
printf 'changed\n' >> "$repo/README.md"

trace="$tmpdir/codex.jsonl"
cat > "$trace" <<'JSONL'
{"type":"response_item","timestamp":"2026-06-16T00:00:00Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"go test ./...\"}"}}
{malformed
JSONL

import_output="$("$tmpdir/agentreceipt" --repo "$repo" import codex-jsonl "$trace")"
mark_output="$("$tmpdir/agentreceipt" --repo "$repo" mark "smoke reviewed")"
stop_output="$("$tmpdir/agentreceipt" --repo "$repo" stop)"
verify_output="$("$tmpdir/agentreceipt" --repo "$repo" verify)"
review_pr_output="$("$tmpdir/agentreceipt" --repo "$repo" review --last --pr)"
review_json_output="$("$tmpdir/agentreceipt" --repo "$repo" review --last --json)"
export_pr_output="$("$tmpdir/agentreceipt" --repo "$repo" export --pr)"
export_json_output="$("$tmpdir/agentreceipt" --repo "$repo" export --json)"
inspect_output="$("$tmpdir/agentreceipt" inspect codex --home "$tmpdir/missing-codex-home")"

[[ "$init_output" == *"Initialized global AgentReceipt storage"* ]]
[[ "$session_id" == ar_ses_* ]]
[[ "$import_output" == *"warnings=1"* ]]
[[ "$mark_output" == *"smoke reviewed"* ]]
[[ "$stop_output" == *"Finalized AgentReceipt session"* ]]
[[ "$verify_output" == *"Receipt valid."* ]]
[[ "$review_pr_output" == *"## AgentReceipt"* ]]
[[ "$review_json_output" == *'"session_id"'* ]]
[[ "$export_pr_output" == *"## AgentReceipt"* ]]
[[ "$export_json_output" == *'"signature_algorithm": "ed25519"'* ]]
[[ "$inspect_output" == *"warning[codex_logs_missing]"* ]]

test -s "$keydir/default.ed25519"
test -s "$keydir/default.pub"
test ! -e "$repo/.agentreceipt"
test ! -e "$repo/.agentreceipt.yml"
session_dir="$(find "$home/repos" -path "*/sessions/$session_id" -type d -print -quit)"
test -n "$session_dir"
test -s "$session_dir/receipt.json"
test -s "$session_dir/review.md"
test -s "$session_dir/signatures/receipt.sig"

codex_home="$tmpdir/codex-home"
codex_session_dir="$codex_home/sessions/2026/06/17"
mkdir -p "$codex_session_dir"
cat > "$codex_session_dir/watch.jsonl" <<JSONL
{"type":"session_meta","timestamp":"2026-06-17T00:00:00Z","payload":{"type":"session_meta","cwd":"$repo"}}
{"type":"response_item","timestamp":"2026-06-17T00:00:01Z","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":{"cmd":"go test ./..."}}}
{"type":"response_item","timestamp":"2026-06-17T00:00:02Z","payload":{"type":"function_call_output","call_id":"call_1","output":"Exit code: 0\nok"}}
JSONL

watch_output="$("$tmpdir/agentreceipt" --color never --repo "$repo" start --watch --codex-home "$codex_home" --watch-existing --watch-interval 1ms --watch-duration 5ms)"
watch_stop_output="$("$tmpdir/agentreceipt" --repo "$repo" stop)"

[[ "$watch_output" == *"codex  watch"* ]]
[[ "$watch_output" == *"codex  ok      run go test ./..."* ]]
[[ "$watch_output" != *$'\033['* ]]
[[ "$watch_stop_output" == *"Finalized AgentReceipt session"* ]]
