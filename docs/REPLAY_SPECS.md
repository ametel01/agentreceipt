AgentReceipt already records signed, hash-chained session evidence, so replay should reconstruct a
  verifier-friendly view of one specific session from events.jsonl, receipt.json, manifest.json, diffs/
  final.patch, and optional provider traces.

  Product Contract

  - Command: agentreceipt replay --session <ar_ses_...> --json
  - --session should be required for verifier-facing replay. Avoid implicit “latest” in loop
    automation.

  - Default output: one JSON object designed for verifier agents.
  - Optional: agentreceipt replay --session <id> --bundle <dir> writes a portable bundle.
  - Replay must not rerun shell commands, reapply patches, call models, or score the agent.
  - It should emit even when evidence is incomplete, with explicit verification.valid, gaps, and
    confidence.

  Verifier Replay JSON
  Top-level shape:

  {
    "schema_version": 1,
    "kind": "agentreceipt.session_replay",
    "session_id": "ar_ses_...",
    "generated_at": "2026-...",
    "source": {
      "agentreceipt_version": "v...",
      "repo_root": "/repo",
      "session_state": "finalized"
    },
    "verification": {
      "valid": true,
      "event_chain_hash": "sha256:...",
      "diff_hash": "sha256:...",
      "manifest_hash": "sha256:...",
      "receipt_hash": "sha256:...",
      "signature_valid": true,
      "signed_by": "embedded:sha256:..."
    },
    "summary": {},
    "timeline": [],
    "commands": [],
    "files": [],
    "risks": [],
    "gaps": [],
    "verifier_tasks": [],
    "artifacts": []
  }

  Required Sections

  - summary: provider, duration, changed file count, command count, tests/lint/typecheck detected,
    final risk.

  - timeline: ordered by event seq; each item has seq, ts, type, source, confidence, and evidence_refs.
  - commands: paired command attempts/results by call_id where possible; include command, kind, status,
    exit_code, risk_signals, output_summary, stdout_truncated.

  - files: repo-relative changed paths, actions, sensitive/dependency flags, whether present in final
    diff.

  - risks: normalized risk codes, levels, confidence, linked commands/files.
  - gaps: missing provider evidence, unknown command status, unpaired command result, missing tests/
    lint/typecheck, invalid/tampered receipt.

  - verifier_tasks: concrete checks for the verifier agent, e.g. “inspect failed command”, “confirm
    tests for code changes”, “review sensitive path”.

  - artifacts: stable references like events.jsonl, receipt.json, manifest.json, diffs/final.patch,
    with hashes.

  Portable Bundle
  agentreceipt replay --session <id> --bundle ./replay should write:

  replay/
    replay.json
    receipt.json
    manifest.json
    events.jsonl
    diffs/
      final.patch
    provider/
      codex/
        traces/   # optional redacted traces only

  Important Defaults

  - No raw prompts.
  - No raw tool output by default.
  - No raw provider logs by default.
  - Redact secrets before replay output.
  - Preserve local-first behavior.
  - Provider evidence remains enrichment; Git/filesystem/receipt integrity stay authoritative.

  Acceptance Criteria

  - A verifier agent can consume replay.json without parsing Markdown.
  - Same finalized session produces stable ordering and stable evidence refs.
  - Tampered events.jsonl, receipt.json, manifest.json, or final.patch marks replay invalid.
  - Missing provider evidence produces a gap, not a crash.
  - Each risk/focus item links back to concrete evidence.
  - The feature reuses existing review, receipt.Verify, and eventlog.Replay logic instead of inventing
    a second verifier.

  This matches the OKF direction: verifier loops need clear evidence, independent checks, durable run
  history, explicit gaps, and persisted verdicts. Key sources: /Users/alexmetelli/source/alex-okf/queries/ai-coding-content/vercel-eve-
  production-agent-framework.md:25, /Users/alexmetelli/source/alex-okf/queries/ai-coding-content/agent-loop-architecture-durable-
  orchestration.md:58, /Users/alexmetelli/source/alex-okf/queries/ai-coding-content/real-world-agent-loop-patterns.md:14, and queries/ai-
  coding-content/loop-memory-engineering.md:101.
