# Codex Trace Extraction Specification for Reports

## 1) Purpose

This specification defines the Codex-first extraction process for evidence reports.

Codex trace extraction is an enrichment source for AgentReceipt, not the core proof mechanism. The core receipt remains valid with Git + filesystem evidence even when Codex logs are missing, malformed, incomplete, or unavailable. This spec therefore defines best-effort provider evidence with explicit confidence labels and parse gaps.

The extractor should produce reviewer-grade outputs that answer:

- What session happened and when
- Who/what executed tools
- Which shell commands were attempted
- Which commands succeeded or failed
- Which files or paths were touched/read/modified by command evidence
- Which commands touched high-risk surfaces
- Which parts of the evidence are high-confidence vs partial

## 2) Source of truth and precedence

### 2.1 Primary when available: session JSONL

- Session index: `~/.codex/session_index.jsonl`
- Session logs: `~/.codex/sessions/YYYY/MM/DD/rollout-YYYY-MM-DDTHH-...jsonl`
- Alternate home paths when `CODEX_HOME` is set:
  - `$CODEX_HOME/session_index.jsonl`
  - `$CODEX_HOME/sessions/**`
  - `$CODEX_HOME/archived_sessions/**`

Use session index to discover candidate session ids, then open matching `sessions/YYYY/MM/DD/*.jsonl` files.

If no session index or matching JSONL file is found, emit an explicit gap record and continue; do not fail receipt finalization.

### 2.2 Secondary: local runtime logs (supplemental)

- `~/.codex/logs_2.sqlite`
  - table: `logs`

Use this source to fill tool-call execution traces and process/thread context when JSONL lacks sufficient operational detail.

### 2.3 Supplemental activity files

- `~/.codex/history.jsonl`
- `~/.codex/log/codex-login.log`

Use for continuity and timeline checks, never as the sole evidence source for tool command details.

### 2.4 Trust rule

1. JSONL `sessions/*` is authoritative for tool+command events.
2. SQLite logs are supplemental (supports correlation and audit confidence).
3. Missing or malformed records are never dropped silently; they are represented as parse warnings or gap records with `low` or `null` confidence and reason codes.
4. Provider evidence must never override high-confidence Git or filesystem evidence when they conflict; conflicts become review warnings.

## 3) Trace objects to capture

When provider evidence is available, the report extractor must output one JSON object per session containing these top-level sections:

1. `session`
2. `timeline`
3. `tool_calls`
4. `commands`
5. `message_blocks`
6. `execution_errors`
7. `risk_signals`
8. `source_confidence`

When provider evidence is unavailable, the extractor must still write a minimal summary and parse report that records the attempted paths, reason for missing evidence, and resulting confidence downgrade.

## 4) Session schema (required)

```json
{
  "session_id": "019ecf4d-7e9a-7753-b5ce-1cda83471a81",
  "source": "cli",
  "originator": "codex-tui",
  "cwd": "/Users/..",
  "model_provider": "openai",
  "model": "gpt-5.3-codex-spark",
  "thread_source": "user",
  "started_at": "2026-06-16T07:20:43.089Z",
  "updated_at": "2026-06-16T07:35:12.000Z",
  "git": {
    "rev_parse_toplevel": null
  },
  "approval_policy": "never",
  "sandbox": "danger-full-access",
  "workspace_roots": ["/Users/alexmetelli/source/agentreceipt"],
  "event_count": 709,
  "message_count": 32,
  "tool_call_count": 68
}
```

## 5) Timeline schema (required)

Each record must represent an ordered event.

```json
{
  "i": 12,
  "ts": "2026-06-16T07:20:43.089Z",
  "type": "event_msg|response_item|turn_context|session_meta|compacted",
  "subtype": "task_started|agent_message|function_call|function_call_output|user_message",
  "turn_id": "019ecf5e...",
  "summary": "short human readable text",
  "raw": { "...": "optional redacted raw json" }
}
```

## 6) Tool-call schema (required)

```json
{
  "session_id": "019ecf4d...",
  "ts": "2026-06-16T07:20:43.120Z",
  "turn_id": "019ecf5e...",
  "tool": "exec_command",
  "tool_type": "function_call",
  "call_id": "call_xGznv...",
  "arguments": {
    "cmd": "rg -n \"pattern\" file.md",
    "workdir": "/Users/..."
  },
  "command": "rg -n \"pattern\" file.md",
  "source": "session_jsonl"
}
```

## 7) Command result schema (required)

```json
{
  "session_id": "019ecf4d...",
  "call_id": "call_xGznv...",
  "turn_id": "019ecf5e...",
  "tool": "exec_command",
  "ts": "2026-06-16T07:20:44.101Z",
  "status": "success|failed|unknown",
  "exit_code": 0,
  "stdout": "first 2000 chars after redaction",
  "stdout_truncated": false,
  "stderr_or_error": null,
  "failed_reason": null,
  "raw_output": "redacted full output when small; else store local blob reference"
}
```

`failed_reason` is extracted from output patterns like:
- `exec_command failed for ...`
- `Exit code: [non-zero]`

## 8) Message block schema (required)

```json
{
  "session_id": "019ecf4d...",
  "ts": "2026-06-16T07:20:44.010Z",
  "kind": "event_msg|response_item",
  "subkind": "user_message|agent_message|developer|system",
  "turn_id": "019ecf5e...",
  "text": "sanitized message text",
  "encrypted_content": false
}
```

For encrypted assistant content where available, capture placeholder metadata only:

```json
{"encrypted_content": true, "redaction_reason": "reason"}
```

## 9) Execution error schema (required)

```json
{
  "session_id": "019ecf4d...",
  "call_id": "call_xGznv...",
  "error_class": "exec_failed|tool_error|output_parse_error|unknown",
  "message": "command tool failure text",
  "severity": "medium|high",
  "ts": "2026-06-16T07:20:44.200Z"
}
```

## 10) Risk signal schema (required)

```json
{
  "session_id": "019ecf4d...",
  "level": "low|medium|high",
  "signal": "destructive_command|external_network|credential_file|repo_escape|repeated_failure|large_file_scan",
  "command": "git ... or rm ...",
  "details": "why flagged",
  "line_no": 17,
  "confidence": "high|medium|low"
}
```

Risk logic is configured by regex on command text and target paths.

### Default risky patterns

- `\brm\b` + absolute or outside-workspace targets
- `\bdd\s+` / `\bmkfs\b` / `\bshutdown\b` / `\breboot\b`
- `curl`, `wget`, `ssh`, `nc`, `openssl`, `aws`, `gcloud`, `npm publish`, `pnpm dlx`, `pip install`
- `chmod 777`, `chmod 666`, `cat .env`, `PRIVATE` / `TOKEN` / `KEY` in command context
- dependency/lockfile writes detected from Git or filesystem evidence
- auth/payment/security/crypto/infra path changes detected from Git or filesystem evidence
- final diff mismatch or receipt verification failure reported by AgentReceipt core

## 11) SQLite trace schema mapping

Map `logs_2.sqlite` rows into optional supplemental event records:

```sql
SELECT ts, ts_nanos, level, target, thread_id, process_uuid, feedback_log_body
FROM logs
WHERE thread_id = :session_id
ORDER BY ts, ts_nanos, id;
```

Optional join keys:
- `session_id == thread_id`
- `turn_id` embedded in log message
- `call_id` embedded as `call_id="..."`

## 12) Extraction commands

Below commands run from project root unless noted otherwise.

### 12.1 Enumerate sessions

```bash
jq -r '"\(.updated_at) \(.id) \(.thread_name)"' ~/.codex/session_index.jsonl | sort
```

### 12.2 Extract one session metadata

```bash
SESSION_FILE=~/.codex/sessions/2026/06/16/rollout-2026-06-16T15-20-22-019ecf4d-7e9a-7753-b5ce-1cda83471a81.jsonl
jq 'select(.type=="session_meta") or select(.type=="turn_context")' "$SESSION_FILE"
```

### 12.3 Extract tool calls and arguments

```bash
jq -c 'select(.type=="response_item" and .payload.type=="function_call") | {ts:.timestamp, type:.payload.type, tool:.payload.name, call_id:.payload.call_id, args:(.payload.arguments|fromjson?)}' "$SESSION_FILE"
```

### 12.4 Extract tool outputs and failures

```bash
jq -c 'select(.type=="response_item" and (.payload.type=="function_call_output" or .payload.type=="custom_tool_call_output")) | {ts:.timestamp, tool:.payload.name, output:.payload.output}' "$SESSION_FILE"
```

### 12.5 Extract failure markers only

```bash
jq -c 'select(.type=="response_item" and (.payload.type=="function_call_output" or .payload.type=="custom_tool_call_output") and ((.payload.output|tostring|contains("failed for")) or ((.payload.output|tostring|match("Exit code: [1-9]")) ))' "$SESSION_FILE"
```

### 12.6 Extract timeline and messages

```bash
jq -c 'select(.type=="event_msg" or (.type=="response_item" and .payload.type=="message") or .type=="turn_context") | {ts:.timestamp, kind:.type, payload_type:(.payload.type // "")}' "$SESSION_FILE"
```

### 12.7 Extract sqlite runtime context for a session

```bash
sqlite3 ~/.codex/logs_2.sqlite \
"SELECT ts, ts_nanos, level, target, thread_id, process_uuid, substr(feedback_log_body,1,240) \
 FROM logs WHERE thread_id='019ecf4d-7e9a-7753-b5ce-1cda83471a81' ORDER BY id DESC LIMIT 200;"
```

## 13) Source confidence model

Use confidence per extracted block:

- `high`: present in `session_jsonl` with structured type (`function_call`, `tool output`)
- `medium`: present in `session_jsonl` text but missing structured payload
- `low`: present only from supplemental sources (`logs_2.sqlite`, `history.jsonl`)
- `null`: missing/incomplete; create explicit gap record

Attach confidence in every row and include reason codes.

## 14) Required report sections

A report generated from this spec must include at least:

1. Session summary block
2. Tool trace table
3. Command trace with success/fail
4. Evidence confidence table
5. Risk signals and rationale
6. Raw exception log (failures + parse warnings)
7. Reproducibility bundle
   - extraction script/commands
   - dataset path


## 15) Output file conventions for parser outputs

Default output location for session-coupled extraction:

```text
~/.agentreceipt/repos/<repo-key>/sessions/<session_id>/provider/codex/traces/
```

Provider parse reports and imported raw logs should remain under:

```text
~/.agentreceipt/repos/<repo-key>/sessions/<session_id>/provider/codex/
```

Standalone research-harness extraction may also write to `codex-traces/<session_id>/`, but those files are diagnostic artifacts and are not the canonical receipt storage location.

- `session-meta.ndjson`
- `timeline.ndjson`
- `tool-calls.ndjson`
- `command-events.ndjson`
- `errors.ndjson`
- `risk-signals.ndjson`
- `session-summary.ndjson`
- `source-confidence.ndjson`
- `parse-report.json`

Each file should be written per session. Large payloads should be represented by content-addressed blob references under the local AgentReceipt session when retention is enabled.

## 16) Redaction and privacy rules

- Redact tokens/keys before persistence if output contains known key patterns:
  - `sk-`, `bearer `, `authorization`, `token=`, `api_key`
- Truncate stdout to safe size for indexing (default 2000 chars)
- Keep full output in secure local file if needed, with reference pointer in NDJSON
- Do not export raw prompts, raw tool outputs, or raw provider records into PR comments by default.
- Preserve unknown records only as redacted opaque records or blob references.

## 17) Validation checklist (must pass before report publish)

- Session index row resolved and file path confirmed, or explicit missing-provider gap recorded
- `session_meta` present and parseable, or explicit low/null confidence reason recorded
- Timeline includes first/last timestamp and ordering monotonic when timeline records are available
- Tool call count matches output/response item count where available
- Every `function_call` has attempted `function_call_output` pairing unless aborted or missing output is recorded as a gap
- Failures are explicitly listed
- Confidence report includes at least one bucket per attempted source
- Risk signals are generated from command/tool-output evidence and reconciled with Git/filesystem evidence
- Extraction failure never blocks receipt finalization; it downgrades provider confidence and creates review warnings
