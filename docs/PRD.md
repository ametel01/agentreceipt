# AgentReceipt PRD

## Current-State Product Requirements

This document describes what the current AgentReceipt codebase implements. It is not a roadmap or aspiration document.

## 1. Product Summary

AgentReceipt is a local-first CLI for recording observable evidence beside normal AI coding sessions and producing signed review receipts.

It does not launch, wrap, proxy, sandbox, approve, deny, control, or score AI agents. Developers keep using Codex CLI or Claude Code normally. AgentReceipt records local evidence from the repository, provider logs or hooks when available, and explicit human markers, then writes verifiable local artifacts for review.

The core product claim remains narrow:

```text
The final diff is not enough evidence.
```

AgentReceipt creates review artifacts that show:

- what files changed
- which provider tool and command events were observed
- which commands, tests, lint, or typecheck signals were detected
- whether sensitive paths or dependency files changed
- whether the current workspace still matches the recorded final diff
- whether the event log, manifest, receipt hash, final patch hash, and signature verify
- where evidence is high confidence, medium confidence, low confidence, or unavailable
- whether the captured session can be replayed as verifier-facing, machine-readable evidence

## 2. Positioning

AgentReceipt is code provenance and PR review infrastructure for AI-assisted code changes.

It is:

- local-first
- sidecar-first
- receipt-first
- PR-review-first
- deterministic before AI-assisted
- evidence-focused rather than score-focused
- useful without a hosted service
- focused on one diff and one session, not on ranking agents

It is not:

- an agent leaderboard
- an agent reputation network
- a verifier-scored benchmark product
- a hosted-first AI scoring dashboard
- a model router
- an agent orchestrator
- an agent permission layer
- a product that relies only on the agent's self-report

The product answers this question:

```text
Can I trust this specific AI-generated diff before I merge it?
```

It does not answer this question:

```text
Which agent should I trust for this workflow?
```

## 3. Target Users

### Individual AI-heavy developer

Uses Codex CLI or Claude Code for local coding work.

Needs:

- no workflow disruption
- no wrapper process around the agent
- no cloud account or API key
- local signed receipts
- fast live or post-session review
- explicit evidence quality

### Senior reviewer or tech lead

Reviews AI-assisted pull requests.

Needs:

- concise receipt summary
- branch and workspace diff context
- final diff verification
- risky file and command signals
- command, test, lint, and typecheck evidence
- reviewer focus prompts
- no opaque trust score

### Security-conscious team

Cares about secrets, dependencies, infrastructure, auth, payments, crypto, and production safety.

Needs:

- local-first operation
- redaction defaults
- signed artifacts
- sensitive path detection
- dependency-change detection
- command-risk classification
- portable receipt verification

## 4. Implemented Scope

Current implemented scope includes:

- Go CLI built with Cobra
- explicit `start` / `stop` session lifecycle
- global AgentReceipt storage under `~/.agentreceipt` by default
- `AGENTRECEIPT_HOME` override for storage
- Ed25519 local signing keys under `~/.agentreceipt/keys` by default
- `AGENTRECEIPT_KEY_DIR` override for signing keys
- Git monitor evidence at session start and stop
- filesystem watcher sidecar evidence while the session is active
- best-effort Codex JSONL parsing and live tailing
- Codex-first `start --watch` live view
- Claude Code hook installation and active-session hook ingestion
- manual signed context markers
- hash-chained `events.jsonl`
- signed `receipt.json`
- rendered `receipt.md` and `review.md`
- detached receipt signature file
- terminal, JSON, Markdown, and PR Markdown review/export modes
- portable receipt verification using embedded signer public key and key ID
- local artifact bundle verification
- machine-readable verifier replay via `agentreceipt replay --session <id>`
- portable replay bundles via `agentreceipt replay --session <id> --bundle <path>`
- GitHub CLI PR comment posting
- release packaging and installer scripts

Current explicit non-scope includes:

- Cursor, Aider, Gemini, JetBrains, and VS Code extension integrations
- hosted dashboard
- GitHub App enforcement
- enterprise policy distribution
- cloud sync
- CI receipt gate enforcement
- agent wrapping or managed execution
- model proxying
- MCP proxying
- network proxying
- eBPF, dtrace, or deep OS tracing
- agent reputation scores
- AI verifier scoring
- multi-agent comparison

## 5. Core Workflow

### Optional Initialization

```bash
agentreceipt init
```

`init` creates global AgentReceipt storage and a default local signing key if needed. It does not write repository-local state.

### Codex Live Watch

```bash
agentreceipt start --watch
```

`start --watch` starts or resumes an active AgentReceipt session, follows matching Codex JSONL logs, prints compact live watch events, and imports parsed provider events into the active receipt.

Useful options:

```bash
agentreceipt start --watch --watch-interval 500ms
agentreceipt start --watch --watch-existing
agentreceipt start --watch --watch-duration 5m
agentreceipt start --watch --codex-home ~/.codex
agentreceipt --color never start --watch
agentreceipt --color always start --watch
```

Pressing `Ctrl-C` stops the foreground Codex watch loop. The AgentReceipt session remains active until `agentreceipt stop` finalizes it.

### Record Without Foreground Watch

```bash
agentreceipt start
```

This starts a local capture session with Git and filesystem evidence. Provider events can still be appended later by Codex import, Codex review import, or Claude hooks while the session is active.

### Status And Event Log

```bash
agentreceipt status
agentreceipt sessions
agentreceipt events
agentreceipt events --limit 50
agentreceipt events --format json
agentreceipt events --format jsonl
```

`status` renders the active session state, capture sources, event count, chain hash, and warnings.

`sessions` lists AgentReceipt sessions available for the current repository, including state, active marker, updated time, event count, and warning count.

`events` renders recent canonical session events as a readable terminal timeline by default, with color controlled by `--color`.

`events --format json` prints an indented JSON array. `events --format jsonl` prints compact newline-delimited JSON for scripts.

### Human Markers

```bash
agentreceipt mark "Manually reviewed generated auth changes"
```

`mark` appends a signed `manual.marker` event to the active hash chain.

### Finalize

```bash
agentreceipt stop
```

`stop` stops the filesystem watcher, captures final Git state, appends a receipt finalizer event, writes finalized state and manifest data, clears the active session pointer, then builds and signs receipt artifacts.

If no provider tool events were observed, finalization still succeeds and records a warning.

### Review

```bash
agentreceipt review
agentreceipt review --last
agentreceipt review --session <id>
agentreceipt review --security
agentreceipt review --diff
agentreceipt review --codex-jsonl ./codex-run.jsonl
agentreceipt review --json
agentreceipt review --md
agentreceipt review --pr
```

`review --codex-jsonl` imports a Codex JSONL trace into the active session before building the review. It requires an active AgentReceipt session.

### Verify

```bash
agentreceipt verify
agentreceipt verify --session <id>
agentreceipt verify bundle ./agentreceipt
```

`verify` checks receipt integrity for a local finalized session.

`verify bundle` checks a local artifact bundle path and does not contact GitHub.

### Replay

```bash
agentreceipt replay --session <id>
agentreceipt replay --session <id> --json
agentreceipt replay --session <id> --bundle ./replay-bundle
```

Replay reconstructs a verifier-facing JSON payload from one finalized session artifact set.

- `--session` is required.
- Output is machine-readable JSON and defaults to `agentreceipt.session_replay` structure.
- `--bundle <path>` writes `replay.json` plus required session artifacts and optional normalized Codex traces for offline verifier consumption.
- Replay is artifact-only: it reads session artifacts, does not rerun commands, and does not call local or remote models.

### Export And PR Comment

```bash
agentreceipt export --json
agentreceipt export --md
agentreceipt export --pr
agentreceipt export --session <id> --json
agentreceipt pr comment
```

`pr comment` requires the `gh` CLI and a current pull request. It exports PR Markdown from the most recent finalized receipt and posts it to the current PR.

## 6. Command Surface

### Global Flags

All commands inherit:

```bash
--color auto|always|never
--config <path>
--quiet
--repo <path>
```

`--repo` selects the repository root to inspect. If omitted, AgentReceipt discovers the Git repository from the current directory.

`--config` loads an explicit YAML config file. AgentReceipt does not require repo-local config by default.

`--color` controls terminal color for supported human-facing output.

### Commands

| Command | Current behavior |
| --- | --- |
| `agentreceipt init` | Creates global storage and default Ed25519 signing keys if needed. |
| `agentreceipt install codex` | Read-only Codex home/log detection. Prints candidate count, warning count, newest candidate when present, and next-step watch guidance. |
| `agentreceipt install codex --home <path>` | Uses a specific Codex home directory instead of `CODEX_HOME` or `~/.codex`. |
| `agentreceipt install claude` | Installs or updates an AgentReceipt hook entry in Claude settings JSON, preserving unrelated settings and backing up existing settings when changed. |
| `agentreceipt install claude --dry-run` | Prints the planned Claude settings change without writing. |
| `agentreceipt install claude --settings <path>` | Targets a specific Claude settings JSON file. |
| `agentreceipt start` | Starts a capture session. Fails if an active session already exists. |
| `agentreceipt start --watch` | Starts or resumes a session and tails matching Codex logs into the active receipt. |
| `agentreceipt status` | Shows the active session state. Prints no-active-session text when none exists. |
| `agentreceipt sessions` | Lists sessions for the current repository. |
| `agentreceipt events` | Shows recent active-session events as a readable timeline. Supports `--format json` and `--format jsonl`. |
| `agentreceipt stop` | Finalizes the active session and signs receipt artifacts. |
| `agentreceipt review` | Builds a reviewer-focused report from a finalized or selected session. |
| `agentreceipt verify` | Verifies local receipt integrity and exits non-zero when invalid. |
| `agentreceipt verify bundle <path>` | Verifies a local artifact bundle and exits non-zero when invalid. |
| `agentreceipt replay` | Builds a machine-readable verifier replay report for `--session <id>`. |
| `agentreceipt replay --session <id> --bundle <path>` | Writes a portable verifier replay bundle and exits non-zero when replay construction or required artifact checks fail. |
| `agentreceipt export --json|--md|--pr` | Exports a finalized receipt in one selected format. |
| `agentreceipt import codex-jsonl <path>` | Parses a Codex JSONL trace. If a session is active, writes Codex traces and appends provider events. Otherwise runs as a preview import. |
| `agentreceipt inspect codex` | Lists local Codex evidence candidates and warnings. |
| `agentreceipt inspect codex --last` | Limits output to the newest Codex candidate. |
| `agentreceipt mark <message>` | Adds a signed manual marker to the active session. |
| `agentreceipt pr comment` | Posts PR Markdown for the latest receipt through `gh pr comment`. |
| `agentreceipt version` | Prints the injected release version, or `dev` in source builds. |

Hidden internal commands currently exist for Claude hook ingestion and filesystem watcher sidecar operation. They are implementation details, not user-facing commands.

## 7. Evidence Sources

### Git Monitor

Git monitor evidence is high confidence for repository state and diffs.

At session start and stop, AgentReceipt records:

- repository root
- current branch
- current HEAD
- dirty status
- Git status entries
- staged diff hash
- unstaged diff hash
- combined patch file
- patch hash

The start patch is written to `diffs/000001.patch`. The final patch is written to `diffs/final.patch`.

### Filesystem Watcher

Filesystem watcher evidence is high confidence for writes that the OS watcher observes.

It records debounced `fs.change` events for create, modify, remove, and rename-style filesystem notifications where supported by `fsnotify`.

Each changed path is classified for:

- sensitive path match
- dependency file match

The watcher captures writes, not file reads.

### Codex Session Logs

Codex support is the primary live provider path.

AgentReceipt reads likely Codex locations under `CODEX_HOME` or `~/.codex`, including session and archived-session areas. Interactive Codex logs are treated as local best-effort evidence, not as a stable provider API.

Codex parser behavior:

- parses JSONL line by line
- preserves parse warnings
- never treats malformed lines as fatal to receipt generation
- extracts provider commands, command results, tool events, token usage, risk signals, and trace records where supported
- redacts secret-like content by default
- omits prompts and raw tool output unless config opts into retention
- writes trace outputs under `provider/codex/traces/` when imported into an active session

`start --watch` prefers Codex logs whose `cwd` metadata matches the current Git repository. Newly created logs without `cwd` metadata may be followed briefly so early tool calls are not missed.

### Claude Hooks

Claude support is an MVP hook ingestion path, not a full Claude live watcher or transcript importer.

`agentreceipt install claude` writes an `agentreceipt` hook entry into Claude settings JSON. The hook command points to the current `agentreceipt __internal-claude-hook` executable path.

The hidden hook command reads one Claude hook JSON record from stdin or `--file`, normalizes it into provider-neutral events, and appends those events to the active session.

Claude hook parser behavior:

- emits `provider.command` for command attempts
- emits `provider.command_result` for command results
- emits `provider.event` for other tool or hook records
- uses `Provider: "claude"`
- correlates attempts and results by call ID when present
- redacts secret-like content by default
- does not retain prompts or raw tool output unless config opts in
- records `claude_`-prefixed warnings for malformed or incomplete records

### Manual Markers

Manual markers are signed local context events. They let a developer add human review context to the active session's event chain.

### Replay (Verifier Surface)

Verifier replay uses existing session artifacts only:

- `events.jsonl`
- `receipt.json`
- `manifest.json`
- `diffs/final.patch`

Replay also includes normalized provider traces under `provider/codex/traces/` where available. By default, raw provider logs are excluded from replay outputs and bundles.

Replay surfaces explicit verifier fields for:

- command attempts/results with status and exit context where available
- changed file evidence references
- risk list and final risk level
- validation status and warnings
- missing evidence gaps
- verifier tasks derived from evidence gaps and risks

## 8. Confidence And Risk

### Confidence Values

Implemented confidence values are:

- `high`
- `medium`
- `low-medium`
- `low`
- `none`

Default review confidence behavior:

- Git diff confidence becomes `high` when Git monitor events exist.
- Filesystem write confidence becomes `high` when filesystem watcher events exist.
- Provider tool event confidence becomes `medium` when provider command, command-result, or provider events exist.
- Provider tool event confidence remains `none` for warning-only provider evidence.
- File read confidence is currently `none`.
- Network call confidence is currently `low`.

### Risk Levels

Implemented risk levels are:

- `info`
- `low`
- `medium`
- `high`
- `critical`

Risk is deterministic and heuristic. It is not a policy engine and not a trust score.

Risk reasons can come from:

- sensitive path changes
- auth path changes
- dependency file changes
- missing tests after code changes
- missing typecheck after TypeScript changes
- provider warnings
- provider risk signals
- command-risk classification
- invalid event-chain replay

Command-risk classification currently flags signals such as:

- privilege escalation
- destructive filesystem operations
- `find -delete`
- credential or secret access
- network egress
- cloud or deploy mutation
- package publishing
- destructive Git operations
- destructive container or cluster operations
- dependency installation
- remote code execution
- database mutation
- Git mutation
- mass edit or overwrite commands
- script runners
- code generation
- broad filesystem reads

Quoted search patterns are stripped before most command-risk matching to reduce false positives.

## 9. Config

AgentReceipt works without a config file. Passing `--config <path>` loads an explicit YAML config.

The current schema version is `1`.

Default config:

```yaml
version: 1
session:
  idle_timeout_minutes: 30
  auto_finalize_on_stop: true
capture:
  git: true
  filesystem: true
  claude_hooks: false
  codex_logs: true
  store_terminal_output: false
  store_provider_raw_logs: true
privacy:
  redact_secrets: true
  store_prompts: false
  store_raw_tool_outputs: false
  max_blob_bytes: 200000
review:
  require_tests_for_code_changes: true
  require_typecheck_for_ts: true
  flag_dependency_changes: true
  flag_auth_changes: true
  flag_secret_paths: true
sensitive_paths:
  - ".env"
  - ".env.*"
  - "*.pem"
  - "*.key"
  - "id_rsa"
  - "id_ed25519"
  - ".npmrc"
  - ".pypirc"
  - ".netrc"
  - ".aws/credentials"
  - ".github/workflows/**"
  - "src/auth/**"
  - "src/payments/**"
  - "src/security/**"
  - "src/crypto/**"
  - "migrations/**"
  - "Dockerfile"
  - "docker-compose.yml"
test_commands:
  - "npm test"
  - "npm run test"
  - "npm run lint"
  - "npm run typecheck"
  - "pnpm test"
  - "pnpm lint"
  - "pnpm typecheck"
  - "yarn test"
  - "staticcheck ./..."
  - "go vet ./..."
  - "tsc --noEmit"
  - "pyright"
  - "cargo test"
  - "pytest"
  - "go test ./..."
  - "make test"
  - "make verify"
```

Config validation rejects unsupported versions, negative idle timeout values, non-positive `max_blob_bytes`, empty `sensitive_paths`, and empty `test_commands`.

## 10. Storage Layout

By default, sessions are stored under `~/.agentreceipt`. `AGENTRECEIPT_HOME` can override the root.

Repository storage is keyed by the SHA-256 hash of the canonical repository root, truncated to 16 hex characters.

Current session layout:

```text
~/.agentreceipt/
  repos/
    <repo-key>/
      active_session
      sessions/
        ar_ses_<timestamp>_<random>/
          events.jsonl
          receipt.json
          receipt.md
          review.md
          manifest.json
          state.json
          fswatcher.pid
          fswatcher.stop
          fswatcher.done
          diffs/
            000001.patch
            final.patch
          provider/
            codex/
              parse-report.json
              traces/
            claude/
          blobs/
          signatures/
            receipt.sig
```

Only files produced by current flows are listed here. The storage layout also creates provider, Claude, blob, and signature directories even when a session does not write files into every directory.

Signing keys default to:

```text
~/.agentreceipt/keys/default.ed25519
~/.agentreceipt/keys/default.pub
```

## 11. Event Model

The event schema version is `1`.

Canonical event shape:

```json
{
  "event_id": "evt_example",
  "session_id": "ar_ses_123_example",
  "seq": 1,
  "timestamp": "2026-06-17T00:00:00Z",
  "source": "git_monitor",
  "type": "git.snapshot",
  "provider": "unknown",
  "cwd": "/repo",
  "payload": {},
  "prev_hash": "sha256:...",
  "event_hash": "sha256:..."
}
```

Known event sources include:

- `git_monitor`
- `fs_watcher`
- `codex_session_log`
- `claude_hook`
- `manual_marker`
- `receipt_finalizer`

Known event types include:

- `git.snapshot`
- `fs.change`
- `provider.command`
- `provider.command_result`
- `provider.event`
- `manual.marker`
- `receipt.finalize`

Each appended event is normalized, assigned a sequence number, linked to the previous event hash, written as canonical JSON, and synced to disk.

The first event links from:

```text
sha256("agentreceipt genesis")
```

## 12. Receipt Model

The receipt schema version is `1`.

Current receipt fields:

```json
{
  "schema_version": 1,
  "session_id": "ar_ses_123_example",
  "created_at": "2026-06-17T00:00:00Z",
  "mode": "sidecar",
  "agent": {
    "provider": "Codex CLI",
    "provider_confidence": "medium"
  },
  "repo": {
    "root": "/repo",
    "branch_start": "",
    "branch_end": "",
    "commit_start": "",
    "commit_end": "",
    "dirty_start": false,
    "dirty_end": true
  },
  "summary": {
    "changed_files": [],
    "detected_commands": [],
    "test_detected": false,
    "lint_detected": false,
    "typecheck_detected": false,
    "duration_seconds": 0
  },
  "capture_confidence": {
    "git_diff": "high",
    "filesystem_writes": "high",
    "provider_tool_events": "medium",
    "file_reads": "none",
    "network_calls": "low"
  },
  "risk": {
    "level": "info",
    "reasons": []
  },
  "verification": {
    "event_chain_hash": "sha256:...",
    "diff_hash": "sha256:...",
    "manifest_hash": "sha256:...",
    "receipt_hash": "sha256:...",
    "signature_algorithm": "ed25519",
    "signer_public_key": "base64...",
    "signer_key_id": "sha256:...",
    "signature": "base64...",
    "valid": true
  },
  "warnings": []
}
```

The actual provider label can be:

- `Codex CLI`
- `Claude Code`
- `Codex CLI + Claude Code`
- `unknown`

Unknown top-level receipt JSON fields are rejected during verification.

## 13. Review Output

Terminal review currently leads with:

- session ID
- detected provider
- session state
- risk level
- branch state
- base branch if found
- ahead/behind counts
- staged, unstaged, and untracked counts
- receipt diff status against the current workspace
- branch and workspace diff stats
- command count
- filesystem write file count
- provider tool event count
- warnings
- reviewer focus prompts

Markdown and PR output are concise review summaries. JSON output emits the structured review report.

`review --security` appends a security-sensitive focus prompt.

`review --diff` appends a final patch hash comparison focus prompt.

## 14. Verification Model

`agentreceipt verify` checks:

- event-chain replay against the receipt event-chain hash
- manifest file hash against the receipt manifest hash
- final patch hash against the receipt diff hash
- current workspace diff hash against the receipt diff hash
- unsigned receipt hash against the receipt hash
- unknown top-level receipt fields
- detached or embedded Ed25519 signature

`agentreceipt replay` checks the same artifact hashes and signature state through the local replay path, but it is intentionally artifact-only and does not require a current workspace diff check.

New receipts embed signer public key and signer key ID. Verification can therefore work without the signer's local key directory. Legacy verification can still fall back to the local public key.

Invalid verification prints a rendered verification result and exits non-zero.

## 15. Privacy And Redaction

Default privacy behavior:

- no cloud upload
- no hosted verifier
- no account or API key
- prompts are not retained by default
- raw tool output is not retained by default
- secret-like command and payload content is redacted by provider parsers
- provider-derived trace files stay local
- prompts and raw tool output are not retained in normalized provider events unless explicit config opts into retention
- PR exports use rendered summaries, not raw transcripts

AgentReceipt proves local artifact integrity. It does not prove human identity, cloud attestation, or that unobserved actions did not occur.

## 16. Current Limitations

Current limitations are:

- Codex live watch depends on best-effort local Codex JSONL parsing.
- Claude support is hook-driven and active-session-only; full transcript import and richer Claude coverage are not implemented.
- Filesystem watcher evidence covers writes, not reads.
- Provider evidence is unavailable when provider logs or hooks are absent, malformed, or not matched to the repository.
- Network activity is not directly monitored.
- AgentReceipt does not enforce policy, permissions, sandboxing, CI gates, or merge blocking.
- `--quiet` exists as a global flag, but not every command has materially different quiet output.
- Review risk is heuristic and deterministic, not a security verdict.
- `verify bundle` verifies local artifacts only; it does not contact GitHub or enforce CI policy.
- `replay` is explicit-session only (`--session <id>`) and intentionally excludes raw prompts, raw tool output, and raw provider logs by default.

## 17. Build, Test, Release

The codebase is a Go module targeting Go 1.26.

Local build:

```bash
go build -o agentreceipt .
```

Primary Make targets include:

```bash
make fmt
make fmt-check
make lint
make test
make test-race
make security
make coverage
make build
make smoke
make verify
make tools
make clean
```

`make verify` runs formatting checks, linting, tests, race tests, security checks, coverage, build, and smoke checks.

CI runs `make verify` on Linux and macOS.

Release tags matching `v*` run the release workflow. The release workflow installs tools, runs `make verify`, extracts release notes from `CHANGELOG.md`, builds Linux and macOS archives for amd64 and arm64, writes `SHA256SUMS`, and publishes GitHub Release assets.

The install script supports latest and pinned release installs:

```bash
curl -fsSL https://ametel.dev/agentreceipt/install.sh | sh
curl -fsSL https://ametel.dev/agentreceipt/install.sh | sh -s -- --version v0.5.0
```
