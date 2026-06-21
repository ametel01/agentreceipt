#!/usr/bin/env bash
set -euo pipefail

packages="$(go list ./...)"
test -n "$packages"
go test ./...
./scripts/test-extract-release-notes.sh
./scripts/test-release-scripts.sh

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

go build -o "$tmpdir/agentreceipt" .
help_output="$("$tmpdir/agentreceipt" --help)"
version_output="$("$tmpdir/agentreceipt" version)"
claude_settings="$tmpdir/claude-settings.json"
claude_output="$("$tmpdir/agentreceipt" install claude --dry-run --settings "$claude_settings")"

[[ "$help_output" == *"install codex"* ]]
[[ "$version_output" == *"agentreceipt"* ]]
[[ "$claude_output" == *"Hook command:"* ]]
[[ "$claude_output" == *"__internal-claude-hook"* ]]
test ! -e "$claude_settings"

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
replay_output="$("$tmpdir/agentreceipt" --repo "$repo" replay --session "$session_id" --json)"
replay_full_output="$("$tmpdir/agentreceipt" --repo "$repo" replay --session "$session_id" --full --json)"
replay_query_output="$("$tmpdir/agentreceipt" --repo "$repo" replay --session "$session_id" --events 1-2 --file README.md --evidence events.jsonl#seq=1 --json)"
replay_bundle_dir="$tmpdir/replay-bundle"
replay_bundle_output="$("$tmpdir/agentreceipt" --repo "$repo" replay --session "$session_id" --json --bundle "$replay_bundle_dir")"
schema_replay_output="$("$tmpdir/agentreceipt" schema replay)"
schema_focus_output="$("$tmpdir/agentreceipt" schema focus)"

if "$tmpdir/agentreceipt" --repo "$repo" replay; then
    echo "expected replay without --session to fail" >&2
    exit 1
fi

set +e
focus_output="$("$tmpdir/agentreceipt" --repo "$repo" focus --session "$session_id" 2>"$tmpdir/focus-session.err")"
focus_status=$?
focus_replay_output="$("$tmpdir/agentreceipt" focus --replay "$replay_bundle_dir/replay.json" 2>"$tmpdir/focus-replay.err")"
focus_replay_status=$?
set -e

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
[[ "$replay_output" == *"\"kind\": \"agentreceipt.session_replay\""* ]]
[[ "$replay_output" == *"\"session_id\": \"$session_id\""* ]]
[[ "$replay_output" == *"\"valid\": true"* ]]
[[ "$replay_output" == *"\"commands\""* ]]
[[ "$replay_output" == *"\"indexes\""* ]]
[[ "$replay_output" == *"\"query\""* ]]
[[ "$replay_output" == *"\"compact\": true"* ]]
[[ "$replay_output" == *"go test ./..."* ]]
[[ "$replay_output" != *"imported-session.jsonl"* ]]
[[ "$replay_output" != *"\"risk_signals\""* ]]
[[ "$replay_output" != *"No lint command detected."* ]]
[[ "$replay_output" != *"No typecheck command detected."* ]]
[[ "$replay_output" == *"\"event_chain_valid\": true"* ]]
[[ "$replay_output" == *"\"final_patch_hash_valid\": true"* ]]
[[ "$replay_output" == *"\"manifest_hash_valid\": true"* ]]
[[ "$replay_output" == *"\"receipt_hash_valid\": true"* ]]
[[ "$replay_output" == *"\"path\": \"README.md\""* ]]
[[ "$replay_output" == *"\"in_final_patch\": true"* ]]
[[ "$replay_full_output" == *"\"timeline\""* ]]
[[ "$replay_full_output" == *"\"normalized_type\""* ]]
[[ "$replay_full_output" == *"\"observed\": true"* ]]
[[ "$replay_query_output" == *"\"selected_events\""* ]]
[[ "$replay_query_output" == *"\"selected_files\""* ]]
[[ "$replay_query_output" == *"\"selected_evidence\""* ]]
[[ "$focus_output" == *"\"kind\": \"agentreceipt.session_focus\""* ]]
[[ "$focus_output" == *"\"session_id\": \"$session_id\""* ]]
[[ "$focus_output" == *"\"review_tasks\""* ]]
[[ "$focus_replay_output" == *"\"kind\": \"agentreceipt.session_focus\""* ]]
[[ "$focus_replay_output" == *"\"session_id\": \"$session_id\""* ]]
[[ "$schema_replay_output" == *"\"title\": \"AgentReceipt Replay Report\""* ]]
[[ "$schema_replay_output" == *"\"const\": \"agentreceipt.session_replay\""* ]]
[[ "$schema_focus_output" == *"\"title\": \"AgentReceipt Focus Report\""* ]]
[[ "$schema_focus_output" == *"\"const\": \"agentreceipt.session_focus\""* ]]
case "$focus_status" in 0|10|20|30|40|50) ;; *) echo "unexpected focus status $focus_status" >&2; exit 1 ;; esac
case "$focus_replay_status" in 0|10|20|30|40|50) ;; *) echo "unexpected replay focus status $focus_replay_status" >&2; exit 1 ;; esac

test -s "$keydir/default.ed25519"
test -s "$keydir/default.pub"
test ! -e "$repo/.agentreceipt"
test ! -e "$repo/.agentreceipt.yml"
session_dir="$(find "$home/repos" -path "*/sessions/$session_id" -type d -print -quit)"
test -n "$session_dir"
verify_diff_output="$("$tmpdir/agentreceipt" --repo "$repo" verify diff --session "$session_id" --against "patch:$session_dir/diffs/final.patch" --json)"
[[ "$verify_diff_output" == *"\"equivalent\": true"* ]]
[[ "$verify_diff_output" == *"\"against\": \"patch:"* ]]
test -s "$session_dir/receipt.json"
test -s "$session_dir/review.md"
test -s "$session_dir/signatures/receipt.sig"
test -s "$replay_bundle_dir/replay.json"
test -s "$replay_bundle_dir/receipt.json"
test -s "$replay_bundle_dir/manifest.json"
test -s "$replay_bundle_dir/events.jsonl"
test -s "$replay_bundle_dir/diffs/final.patch"
if test -d "$replay_bundle_dir/provider/codex/traces"; then
    trace_file_count="$(find "$replay_bundle_dir/provider/codex/traces" -type f | wc -l | tr -d ' ')"
    if [[ "$trace_file_count" -gt 0 ]]; then
        :
    fi
fi

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
