# AgentReceipt

AgentReceipt is a **local-first CLI** that creates a verifiable receipt for AI-assisted code changes before you merge a PR.

It works beside your normal AI coding workflow (no wrapper, no proxy, no agent orchestration) and records observable evidence from your workspace to help you review **what changed, why it changed, and how trustworthy that evidence is**.

## Why this exists

In AI-assisted workflows, the final diff is often not enough to answer key safety questions:

- Which files changed and how?
- Did the session include risky paths (auth, security, deployments)?
- Were tests/lint/typecheck commands detected?
- Did the final diff match what was observed during the session?
- Did we lose evidence because logs were incomplete?

AgentReceipt answers those questions with a **local, signed review artifact** you can attach to PRs.

## What is included in this MVP

AgentReceipt MVP focuses on:

- **Codex-first support** as the primary provider path
- explicit session recording with `start` / `stop`
- `git` + filesystem evidence as high-confidence sources
- best-effort Codex session-log enrichment (non-blocking)
- hash-chained event log
- receipt signing (Ed25519)
- local review + verification + export
- concise, no-noise output designed for code review

Planned for later:

- Claude hook integration as a first-class provider path
- GitHub App and wider team workflow enforcement

## Core workflow

Build from source during the MVP:

```bash
go build -o agentreceipt .
```

### 1) Optional global setup

```bash
agentreceipt init
```

`init` only creates global AgentReceipt storage and signing keys. It does not write config or state into your repository.

### 2) Start a session in any git repo

```bash
agentreceipt start
```

Then run Codex normally in your terminal.

For live Codex visibility while the session is active, run:

```bash
agentreceipt start --watch
```

`--watch` keeps AgentReceipt in the foreground, follows matching Codex JSONL session logs, prints tool calls and command results as they appear, and imports those provider events into the active receipt. Press `Ctrl-C` to stop watching; the receipt session remains active until you run `agentreceipt stop`.

Watch output is human-readable by default and backed by structured watch events so later machine-readable rendering can reuse the same event shape. Color is controlled with `--color auto|always|never`; `auto` enables color only for terminal output.

Useful watch options:

```bash
agentreceipt start --watch --watch-interval 500ms
agentreceipt start --watch --watch-existing
agentreceipt start --watch --codex-home ~/.codex
agentreceipt --color never start --watch
agentreceipt --color always start --watch
```

When multiple Codex sessions exist, AgentReceipt prefers logs whose Codex `cwd` metadata matches the current git repository. Newly created Codex logs without `cwd` metadata are followed briefly so early tool calls are not missed.

### 3) End session and finalize

```bash
agentreceipt stop
```

### 4) Review the receipt

```bash
agentreceipt review
```

### 5) Verify integrity

```bash
agentreceipt verify
```

### 6) Export for PRs

```bash
agentreceipt review --pr
agentreceipt review --json
agentreceipt review --md
agentreceipt export --md
agentreceipt export --json
agentreceipt export --pr
agentreceipt pr comment
```

## Important behavior (MVP decisions)

- **Session mode is explicit**: use `start` and `stop`.
- `start` fails fast if core monitors cannot initialize.
- If Codex provider events are missing, the receipt still finalizes with a warning and reduced provider confidence.
- Final diff mismatch or verification issues are surfaced as warning-level risk in review and invalid verification in `verify`, not a hard failure by default.
- AgentReceipt does not write `.agentreceipt`, `.agentreceipt.yml`, or policy files into the repository. Session artifacts live under global AgentReceipt storage.
- Zero trust-by-default: no cloud dependency, no account required, no prompt upload by default.

## How evidence is captured

AgentReceipt uses three sources in this order:

1. **Git monitor (high confidence)**
   - start/end commit, branch, diffs, snapshots
2. **Filesystem watcher (high confidence)**
   - create / modify / delete / rename events and changed paths
3. **Codex session logs (best effort)**
   - parsed when available, best effort only
   - extracts tool calls, shell commands, and file-targeted actions when present

## What you get from `agentreceipt review`

The review focuses on actionable review questions:

- changed files and summary
- detected commands (tests/lint/typecheck when available)
- tool calls the agent attempted (edit/read/write, command, tests/lint/typecheck)
- whether sensitive paths changed
- whether external risks were detected
- whether final diff and recorded evidence align
- confidence tags for each evidence source
- explicit gaps / missing evidence

## What tools the agent called

`agentreceipt review` includes a dedicated section for observed tool usage:

- shell commands (for example: `npm test`, `go test`, `pnpm lint`)
- file/tool operations (for example: read/edit/write/delete where parser support exists)
- command orchestration patterns (Bash/exec-type calls)
- timestamps and sequence for replayable review

If provider logs are unavailable, review shows:

- no tool-call evidence from provider logs
- reduced provider confidence
- no hard failure in receipt generation

## Risk and confidence model

- **Risk levels**: `info`, `low`, `medium`, `high`, `critical`
- **Default confidence model**
  - Git diff: high
  - Filesystem writes: high
  - Codex session logs: medium (best effort)
  - File reads: low-medium
  - Network calls: low

## Storage layout

Receipts are kept locally in global AgentReceipt storage, keyed by repository path:

```text
~/.agentreceipt/
  repos/
    <repo-key>/
      sessions/
        ar_ses_...
          events.jsonl
          receipt.json
          receipt.md
          review.md
          manifest.json
          diffs/
            000001.patch
            final.patch
          signatures/
            receipt.sig
```

## Privacy and redaction

By default:

- raw prompts are not exported
- raw tool outputs are not exported
- secrets are redacted in exports
- raw logs remain local unless explicitly configured

## Quick command reference

```bash
# Setup
agentreceipt init # optional global storage/key setup

# Session lifecycle
agentreceipt start
agentreceipt start --watch
agentreceipt status
agentreceipt live
agentreceipt stop

# Review & checks
agentreceipt review
agentreceipt review --last
agentreceipt review --session <id>
agentreceipt review --security
agentreceipt review --diff
agentreceipt review --json
agentreceipt review --md
agentreceipt review --pr
agentreceipt verify
agentreceipt verify --session <id>

# Exports
agentreceipt export --json
agentreceipt export --md
agentreceipt export --pr
agentreceipt export --session <id> --json

# Parsers
agentreceipt inspect codex --last
agentreceipt import codex-jsonl ./codex-run.jsonl

# Human context and PRs
agentreceipt mark "Manually reviewed generated auth changes"
agentreceipt pr comment
```

> Note: `agentreceipt install claude` exists for roadmap readiness, while Codex is the MVP primary path.

## Example review output (what to look for)

```text
AgentReceipt Review

Session: ar_ses_01J...
Provider: Codex CLI
Capture quality: medium
Risk: medium

Summary:
- Modified 7 files
- Ran npm test
- No secret file changes detected
- Final diff matches captured workspace state

Warnings:
- No typecheck command detected (expected if not run)
- Codex provider events unavailable during session
```

## Where this fits in your workflow

Use it as a deterministic review step:

1. Run a normal AI session.
2. Stop and review the receipt.
3. Attach the generated review artifact to your PR.
4. Verify before merge if required by your team.

This is not a "model score." It is a **receipts-first** check: evidence, integrity, and review context.
