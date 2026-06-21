---
name: agentreceipt
description: Use AgentReceipt CLI as a local evidence sidecar for AI coding sessions. Use when an agent needs compact replay/focus reports, patch-equivalence checks, schema outputs, or deterministic JSON workflows.
---

# AgentReceipt CLI

AgentReceipt records local evidence for AI-assisted code changes and emits machine-readable session reports. Prefer JSON contract commands over terminal prose or Markdown when acting as an agent.

## Core rules

- Run commands from the repository root, or pass `--repo <path>`.
- Use explicit session IDs for agent loops. Get them with `agentreceipt sessions`.
- Treat `focus` as the bounded machine work queue and retry contract.
- Treat `replay` as the factual evidence record.
- Treat `verify diff` as the patch-equivalence gate.
- Do not treat AgentReceipt as a sandbox, policy engine, approval system, or model scorer.
- Prefer JSON contract commands (`focus`, `replay`, `schema`, `verify diff`) over parsing human-oriented review output.
- Preserve stdout from `focus` even when the command exits non-zero; non-zero focus statuses still emit valid JSON.

## Standard agent loop

```bash
agentreceipt sessions
agentreceipt focus --session <id> > focus.json
status=$?
```

Interpret status and `focus.json` together.

### Exit codes

- `0`: pass
- `10`: review required
- `20`: blocker evidence, failed gates, or failed commands
- `30`: integrity failure
- `40`: authenticity or trust unverifiable
- `50`: final patch/workspace diff mismatch
- `60`: invalid CLI input

Focus fields to inspect first:

- `verdict`
- `process_contract`
- `reviewability`
- `agent_tasks`
- `recommended_next_commands`
- `reviewable_files`
- `suppressed_changes`
- `evidence_index`

## Deep evidence

Use replay when focus is not enough:

```bash
agentreceipt replay --session <id> > replay.json
agentreceipt replay --session <id> --events 80-120
agentreceipt replay --session <id> --file internal/replay/focus.go
agentreceipt replay --session <id> --evidence events.jsonl#seq=88
agentreceipt replay --session <id> --full
```

Replay is factual and artifact-only. It reports evidence, verification, quality gates, policy checks, patch summaries, privacy, claims, outcome, and indexes without making scoring or enforcement decisions.

Use schemas for parser or validator workflows:

```bash
agentreceipt schema replay
agentreceipt schema focus
```

## Patch equivalence

```bash
agentreceipt verify diff --session <id> --against merge-base --json
agentreceipt verify diff --session <id> --against HEAD --json
agentreceipt verify diff --session <id> --against patch:/path/to/final.patch --json
agentreceipt verify diff --bundle ./agentreceipt --against pr.patch --json
```

## Capture workflow

Start and stop sessions when capturing:

```bash
agentreceipt init
agentreceipt start --watch
agentreceipt stop
agentreceipt start
agentreceipt stop
```

`stop` attempts final Codex evidence import before finalizing. Use provider install commands only for setup:

```bash
agentreceipt install codex
agentreceipt install claude --dry-run
agentreceipt install claude
```

## Supporting commands

- `agentreceipt status`
- `agentreceipt events --limit 50 --format jsonl`
- `agentreceipt inspect codex --last`
- `agentreceipt import codex-jsonl <path>`
- `agentreceipt mark "message"`
- `agentreceipt verify --session <id>`
- `agentreceipt verify bundle <path>`
- `agentreceipt review --json --session <id>`
- `agentreceipt export --json --session <id>`
- `agentreceipt version`

## Common mistakes

- Do not call replay without `--session`.
- Do not pass both `--session` and `--replay` to focus.
- Do not parse `review`, `export --md`, or `pr comment` output when contract JSON commands are available.
- Do not ignore non-zero focus output.
- Do not run hidden `__internal-*` commands directly.
