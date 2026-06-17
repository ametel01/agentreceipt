# Claude Provider Design

## Purpose

This document defines the Claude Code provider integration contract for AgentReceipt. The current implementation includes an MVP `agentreceipt install claude` settings merge and hidden hook-ingestion command; full transcript import and richer hook schemas remain future work.

The design keeps Claude support aligned with the current Codex evidence model:

- Git and filesystem evidence remain the core high-confidence proof.
- Provider evidence enriches review summaries and receipt confidence.
- Missing or malformed provider evidence creates warnings and confidence downgrades, not finalization failure.
- AgentReceipt runs beside the agent; it must not wrap, proxy, or control Claude execution.

## Event Normalization

Claude ingestion should normalize hook or transcript records into the existing `model.Event` shape so review logic stays provider-neutral.

### Command Attempt

Use `Type: "provider.command"` and `Provider: "claude"` for shell/tool records that attempt a command.

Required payload fields:

- `tool_call.call_id`: stable hook or transcript call identifier when available.
- `tool_call.tool`: Claude tool name, such as a shell or edit tool.
- `tool_call.command`: shell command text when the tool runs a command.
- `tool_call.arguments`: redacted structured arguments when useful for review.

The review summary should derive detected commands from the same `tool_call.command` or `tool_call.arguments.cmd` fields used for Codex.

### Command Result

Use `Type: "provider.command_result"` and `Provider: "claude"` for command completion records.

Required payload fields:

- `command_result.call_id`: identifier matching a command attempt.
- `command_result.status`: `success`, `failed`, or `unknown`.
- `command_result.exit_code`: numeric exit code when Claude exposes one.
- `command_result.stdout_truncated`: whether retained output was truncated.
- `command_result.failed_reason`: concise failure reason when available.

The status values must match the Codex status contract so review can correlate attempts and results without provider-specific branches.

### File Or Tool Event

Use `Type: "provider.event"` and `Provider: "claude"` for non-command tool events, permission prompts, hook lifecycle records, and opaque transcript records.

Recommended payload fields:

- `tool_call.call_id`
- `tool_call.tool`
- `tool_call.arguments`
- `category`: stable local category such as `file_edit`, `permission`, `hook_lifecycle`, or `opaque`.
- `raw_type`: original Claude record type when storing it is safe.

File writes should still be validated against filesystem watcher evidence. Provider file events enrich context but do not replace filesystem evidence.

### Parse Warning

Parse warnings should be stored in the session warnings list with `claude_`-prefixed codes, such as `claude_malformed_json`, `claude_missing_call_id`, or `claude_unknown_hook_record`.

Warnings must include a short message that can be shown in review output without raw prompt or full transcript content.

### Confidence Record

Claude confidence should be computed from observed normalized events:

- Claude hook command/tool events: high confidence when records have stable call IDs and command-result status.
- Claude transcript-only evidence: medium confidence unless hook coverage confirms the same events.
- Warning-only or missing Claude evidence: none for provider tool events.

Git and filesystem confidence remain independent of Claude confidence.

## Storage And Privacy

Claude artifacts should use the existing provider storage area under each session:

```text
provider/claude/
  parse-report.json
  hook-events.jsonl
  transcript.jsonl
  traces/
```

`parse-report.json` should summarize record counts, warning counts, supported hook versions, and confidence. `hook-events.jsonl` should contain raw hook records only when raw provider retention is enabled. `transcript.jsonl` should contain imported transcript records only when explicitly retained.

Privacy rules:

- AgentReceipt must not upload hooks, transcripts, prompts, tool outputs, or diffs.
- Normalized event payloads must be redacted before they enter `events.jsonl`.
- Raw Claude hook and transcript files stay local and are not exported to PR comments by default.
- Prompt text should not be stored in normalized events by default.
- Large raw outputs should be truncated or represented by local blob references, following the Codex trace retention pattern.

## Install Command Contract

`agentreceipt install claude` behavior must be explicit and auditable.

The command must:

- report that it is installing Claude Code hooks and name the target settings file or hook directory;
- support a dry-run or explicit path mode before writing config;
- merge with existing Claude settings without deleting unrelated hooks;
- print every file it created or modified;
- create backups or explain how rollback works;
- validate that the installed hook command resolves to the current `agentreceipt` binary.

The command must not:

- silently mutate shell startup files;
- silently overwrite Claude settings;
- enable prompt or transcript retention without clear output;
- make Claude support appear active when hook installation failed validation.

The MVP installer writes an `agentreceipt` hook entry into a target Claude settings JSON file, supports `--dry-run`, preserves unrelated settings and hooks, backs up changed settings, and points the hook at the current `agentreceipt __internal-claude-hook` command.

## MVP Acceptance Criteria

Claude provider MVP is complete when:

- `agentreceipt install claude --dry-run` shows exact hook changes without writing them.
- `agentreceipt install claude` installs or updates hooks idempotently and reports changed files.
- Hook events are imported into the active session as provider-neutral `model.Event` records.
- Command attempts and results correlate by `call_id`.
- Review summaries show Claude command status using the same detected-command model as Codex.
- Missing Claude evidence is visible but non-fatal.
- Raw hook or transcript content stays local and is excluded from PR exports by default.

## Open Questions

- Which additional Claude Code hook payload fields are stable enough to treat as a local contract?
- Are command result records guaranteed to include the same call identifier as command attempt records?
- Which Claude settings paths need platform-specific handling?
- Can hook installation validate permissions without triggering Claude permission prompts?
- Should transcript import be a separate command from hook ingestion, or a fallback source for the same provider?
