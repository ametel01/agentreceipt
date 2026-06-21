# AI Agent Command Consumption Improvements

This note records proposed improvements for `agentreceipt replay` and `agentreceipt focus` when the primary consumer is another coding or evaluator agent.

## Command Roles

`focus` should be a small, bounded, task-oriented review plan. It should help an evaluator agent decide whether to inspect, rerun validation, reject, or escalate.

`replay` should be the complete evidence record. It should remain artifact-only, factual, and queryable without forcing consumers to ingest every event inline.

## Shared Contract Improvements

Prefer structured enums and reason codes over prose-only fields. For example, replace a task question such as:

```json
{
  "question": "No command evidence was available to determine whether tests ran after code changes."
}
```

with structured data:

```json
{
  "kind": "missing_validation",
  "gate": "tests",
  "after_code_changes": true,
  "status": "unknown",
  "reason_code": "no_command_evidence"
}
```

Add a stable process contract to the JSON payload:

```json
{
  "process_contract": {
    "exit_code": 10,
    "meaning": "review_required",
    "retryable": false
  }
}
```

Recommended exit-code meanings:

- `0`: no review required
- `10`: review required
- `20`: evidence incomplete or corrupt
- `30`: session not found
- `40`: verification failed

## Focus Improvements

Make `focus` the compact work queue for agents. Its default output should stay bounded and should not include a full event timeline.

Recommended additions:

```json
{
  "agent_tasks": [
    {
      "id": "missing_tests",
      "priority": "P1",
      "action": "run_validation",
      "commands": [
        {
          "cwd": "/Users/alexmetelli/source/agentreceipt",
          "argv": ["go", "test", "./..."],
          "purpose": "verify changed Go code"
        }
      ],
      "blocks_verdict": true
    }
  ]
}
```

Recommended behavior:

- Deduplicate review tasks by stable keys such as `kind`, `gate`, `path`, and `reason_code`.
- Separate `source_changes`, `test_changes`, `doc_changes`, `generated_changes`, and `transient_changes`.
- Suppress temp files, backup files, build binaries, and other low-review-value artifacts by default, while preserving them under `suppressed_changes`.
- Add `recommended_next_commands` with `cwd`, `argv`, and rationale.
- Add `reviewable_files` sorted by risk and expected review value.
- Replace repeated evidence refs with compact ranges or a separate evidence index.
- Add a single final routing enum such as `review_required_due_to_missing_command_evidence`.

For a session with no provider command evidence, `focus` should make that routing decision explicit:

```json
{
  "verdict": "review_required",
  "primary_blocker": "no_provider_command_events",
  "reviewable": false,
  "next_actions": [
    "inspect_final_diff",
    "rerun_tests",
    "rerun_lint_or_format",
    "classify_transient_files"
  ]
}
```

## Replay Improvements

Keep `replay` comprehensive, but make the default response token-efficient and queryable.

Instead of dumping the full timeline by default, emit indexes and artifact references:

```json
{
  "summary": {},
  "indexes": {
    "events": {
      "count": 825,
      "artifact": "events.jsonl",
      "sha256": "..."
    },
    "timeline_ranges": [
      {
        "range": "1-80",
        "kind": "setup_or_build_artifacts"
      },
      {
        "range": "81-160",
        "kind": "source_edits"
      }
    ]
  }
}
```

Recommended query surfaces:

```bash
agentreceipt replay --session <id> --events 80-120
agentreceipt replay --session <id> --file cmd/root.go
agentreceipt replay --session <id> --evidence events.jsonl#seq=88
```

Recommended behavior:

- Include raw evidence as artifact references, not inline payload, by default.
- Add `--full` for exhaustive replay output.
- Add `--compact` as the default agent-friendly response.
- Normalize event types such as `command.run`, `command.result`, `file.change`, `git.snapshot`, and `validation.detected`.
- Distinguish observed facts from inferred claims.
- Resolve contradictions between changed-file counters, patch summaries, and workspace summaries.
- Add causal links from file changes to commands and from validation commands to preceding edits.
- Represent missing evidence as structured data instead of repeated prose.

## Reviewability Object

The most important shared addition is a top-level reviewability object:

```json
{
  "reviewability": {
    "status": "partial",
    "blocking_gaps": ["provider_command_events_missing"],
    "can_evaluate_integrity": true,
    "can_evaluate_code_quality": false,
    "requires_rerun_validation": true
  }
}
```

This lets an evaluator agent immediately decide whether to continue reviewing, rerun validation, reject the receipt as incomplete, or ask for external evidence.
