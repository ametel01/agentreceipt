# AgentReceipt PRD v0.2

## Local-First Verifiable Receipts for AI-Generated Pull Requests

## 1. Product Summary

AgentReceipt is a local-first sidecar review layer for AI coding agents.

It runs beside normal Codex CLI sessions in MVP. It does not launch, wrap, proxy, control, or score agents. Developers keep using their agent workflows normally. AgentReceipt records observable evidence during the session and generates a signed receipt that helps a developer or reviewer decide whether an AI-generated diff is safe to merge.

The product is built around one narrow claim:

The final diff is not enough evidence.

AgentReceipt creates a verifiable receipt showing:

* what files changed
* when they changed
* which tool events were observed
* which commands were detected
* whether tests/lint/typecheck were run
* whether sensitive files or risky paths changed
* whether dependencies changed
* whether the final diff matches the recorded session
* whether the receipt was modified after generation
* what evidence is high-confidence vs best-effort

## 2. Strategic Context

The market already has products focused on agent scoring, trust profiles, and recommendations.

AgentReceipt should avoid that lane.

AgentReceipt is not:

* an agent leaderboard
* an agent reputation network
* a verifier-scored benchmark product
* a tool that tells teams which agent to use
* a hosted-first AI scoring dashboard
* a product that relies on the agent self-reporting its own work

AgentReceipt is:

* local-first
* PR-review-first
* receipt-first
* deterministic before AI-assisted
* evidence-focused rather than score-focused
* useful even without cloud
* focused on verifying a specific diff, not ranking agents

## 3. Core Positioning

### One-liner

AgentReceipt gives every AI-generated pull request a signed, local-first receipt.

### Short positioning

Use Codex CLI normally. AgentReceipt records observable evidence beside the session and generates a signed review artifact before merge.

### Category

AI-generated code provenance and PR review infrastructure.

### Core message

Every AI-generated PR should come with a receipt.

### Competitive contrast

Worldline-style products answer:

```text
Which agent should I trust for this workflow?
```

AgentReceipt answers:

```text
Can I trust this specific AI-generated diff before I merge it?
```

## 4. Product Philosophy

AgentReceipt should be boring, verifiable, and useful in code review.

It should prefer deterministic facts over synthetic trust scores.

Good AgentReceipt outputs:

```text
Receipt: valid
Final diff: matches recorded final workspace state
Capture confidence: high for git/filesystem, medium for Codex logs (best effort)
Tests detected: npm test
Risk: medium
Reason: auth files changed and no typecheck detected
```

Bad AgentReceipt outputs:

```text
Agent score: 87/100
Trustworthiness: excellent
Reasoning: 94%
```

AgentReceipt should avoid pretending that a score is proof.

## 5. Problem

AI coding agents can perform many actions that are not sufficiently visible during code review:

* read files
* edit files
* create files
* delete files
* run shell commands
* run tests
* install dependencies
* use MCP tools
* perform web searches
* call external services
* modify CI/CD
* change security-sensitive code
* produce a final diff after hidden intermediate work

The developer often reviews only the final diff. That loses important context:

* Did the agent touch `.env`?
* Did it modify auth, payment, or deployment code?
* Did it run tests?
* Did it run destructive shell commands?
* Did it change dependencies?
* Did the final diff match the recorded session?
* Did provider logs support the claimed actions?
* Did the agent work through normal visible tooling or were there gaps?

Teams then misdiagnose failures as prompt or model problems when the real issue is poor observability.

## 6. Target Users

## 6.1 Individual AI-heavy developer

Uses Claude Code or Codex CLI daily.

Needs:

* no workflow disruption
* no wrapper
* no cloud required
* local signed receipts
* fast post-session review
* useful PR comments
* evidence quality clearly stated

## 6.2 Senior reviewer / tech lead

Reviews AI-assisted PRs.

Needs:

* concise summary
* final diff verification
* list of risky files
* command/test evidence
* clear reviewer checklist
* no noisy dashboard
* no vague trust score

## 6.3 Engineering lead / CTO

Wants to let the team use AI agents without making code review blind.

Needs:

* standard AI PR receipt format
* team policy later
* GitHub checks later
* searchable receipt history later
* no mandatory code upload

## 6.4 Security-conscious team

Cares about secrets, dependencies, infrastructure, auth, payments, and production safety.

Needs:

* local-first mode
* redaction
* signed receipts
* sensitive path detection
* compliance-friendly artifacts
* self-hosted path later

## 7. MVP Scope

## 7.1 Included

MVP includes:

* Codex CLI support
* Codex-first capture priority
* local sidecar session recording
* post-session review
* local signed receipts
* local verification
* git diff timeline
* filesystem write monitoring
* Codex session-log importer, best-effort
* deterministic risk rules
* Markdown and JSON export
* PR comment generation
* no hosted dependency

Planned but not required for current MVP:

* Claude Code support

## 7.2 Excluded

MVP excludes:

* Cursor
* Aider
* Gemini
* JetBrains
* VS Code extension
* hosted dashboard
* GitHub App
* enterprise self-hosting
* agent wrapping
* agent orchestration
* model proxying
* MCP proxying
* network proxying
* eBPF/dtrace/deep OS tracing
* blockchain
* agent reputation scores
* AI verifier scoring
* multi-agent comparison

## 8. Non-Negotiable Product Decisions

## 8.1 No wrapped mode

AgentReceipt must not run:

```bash
agentreceipt run -- claude
agentreceipt run -- codex
```

Commercial harnesses already optimize execution, permissions, streaming, context, resume, and terminal UX. Wrapping them risks degrading the experience and puts AgentReceipt in the wrong category.

AgentReceipt runs beside the agent, not in front of it.

## 8.2 No scoring-first product

AgentReceipt should not create five-dimension agent scores, trust profiles, or routing recommendations in MVP.

The receipt can include:

* risk level
* capture confidence
* verification status
* policy warnings

But not vague behavioral ratings.

## 8.3 Local-first by default

AgentReceipt must be useful without an account, cloud sync, hosted verifier, or API key.

Default mode:

```text
local only
raw logs local only
no code upload
no prompt upload
no cloud verifier
```

## 8.4 Evidence confidence must be explicit

AgentReceipt must not imply it saw everything.

Every receipt should show capture confidence by source:

```text
Git diff: high
Filesystem writes: high
Claude tool events: high
Codex provider logs: medium
File reads: medium/low depending on source
Network calls: low unless instrumented
```

## 9. Core Workflow

## 9.1 Setup

```bash
agentreceipt init
agentreceipt install codex
agentreceipt install claude
```

## 9.2 Normal session

```bash
agentreceipt start
```

User works normally:

```bash
claude
```

or:

```bash
codex
```

Optional live view:

```bash
agentreceipt status
agentreceipt live
```

Finalize:

```bash
agentreceipt stop
```

Review:

```bash
agentreceipt review
```

Export:

```bash
agentreceipt export --md
agentreceipt export --json
agentreceipt verify
```

Generate PR comment:

```bash
agentreceipt review --pr
```

## 10. Main Commands

## 10.1 `agentreceipt init`

Initializes AgentReceipt for the repo.

Creates global AgentReceipt storage and signing keys. It does not create files in the repository.

```text
~/.agentreceipt/
  repos/
```

Creates local signing key if missing:

```text
~/.agentreceipt/keys/default.ed25519
~/.agentreceipt/keys/default.pub
```

## 10.2 `agentreceipt install claude` (deferred)

Installs Claude Code hook integration. In the Codex-first MVP, this is deferred to roadmap after stable Codex-first validation.

Responsibilities:

* locate Claude Code settings
* back up existing settings
* merge with existing hooks safely
* add AgentReceipt hook commands
* validate hook installation
* run a local dry check

Expected output:

```text
Claude Code integration installed.

Hooks configured:
- SessionStart
- UserPromptSubmit
- PreToolUse
- PostToolUse
- PermissionRequest
- Stop
```

## 10.3 `agentreceipt install codex`

Installs Codex support.

Responsibilities:

* detect Codex home/config paths
* locate likely session-log directories
* check readable session logs
* configure Codex parser settings
* warn that Codex provider log support is best-effort
* leave Codex execution behavior untouched

Expected output:

```text
Codex support installed.

Capture:
- Git monitor: high confidence
- Filesystem watcher: high confidence
- Codex session-log import: best effort
- Live Codex tool extraction: experimental
```

## 10.4 `agentreceipt start`

Starts sidecar recording.

Captures initial state:

* repo root
* branch
* HEAD commit
* staged diff
* unstaged diff
* untracked files
* dirty state
* installed integrations
* active policy config

Starts:

* git monitor
* filesystem watcher
* Codex session-log watcher, if available
* append-only event log
* receipt hash chain

Output:

```text
AgentReceipt started.

Session: ar_ses_01J...
Repo: acme/api
Branch: feature/auth-refactor
Mode: sidecar

Capture:
- Git monitor: active
- Filesystem watcher: active
- Codex session import: best effort
```

## 10.5 `agentreceipt status`

Shows current session state.

Example:

```text
AgentReceipt status

Session: ar_ses_01J...
Duration: 14m 12s
Repo: acme/api
Branch: feature/auth-refactor

Detected:
- Codex activity: no

Events:
- File changes: 12
- Git snapshots: 6
- Codex provider events: 0
- Risk events: 2

Risk: medium
```

## 10.6 `agentreceipt live`

Shows live activity.

Example:

```text
AgentReceipt live

[12:01:04] Codex: Read src/auth/session.ts
[12:01:08] Codex: Edit src/auth/session.ts
[12:01:09] File modified: src/auth/session.ts
[12:01:12] Git diff snapshot: +28 -11
[12:02:01] Codex: Bash npm test
[12:02:13] Test command detected: npm test

Current risk: medium
Reason: auth-sensitive file changed
```

## 10.7 `agentreceipt stop`

Stops sidecar recording and finalizes the receipt.

Actions:

* stop watchers
* close event log
* capture final git state
* capture final diff
* compute final diff hash
* compute event-chain hash
* run risk rules
* sign receipt
* write receipt JSON
* write Markdown summary
* write review report
* emit warning if no Codex events were observed during session

Output:

```text
AgentReceipt finalized.

Session: ar_ses_01J...
Receipt hash: sha256:...
Signature: valid
Risk: medium

Files changed: 7
Commands detected: 4
Tool events: 31
Tests detected:
- npm test

Receipt:
~/.agentreceipt/repos/<repo-key>/sessions/ar_ses_01J/receipt.md
```

If no Codex provider events are observed, finalization still succeeds; review surfaces a confidence warning and continues with a medium provider confidence signal.

## 10.8 `agentreceipt review`

Primary product surface.

Modes:

```bash
agentreceipt review
agentreceipt review --last
agentreceipt review --session <id>
agentreceipt review --security
agentreceipt review --diff
agentreceipt review --pr
agentreceipt review --json
```

Must answer:

* What changed?
* Which files changed?
* Which tool events were observed?
* Which commands were detected?
* Which tests/lint/typecheck commands were detected?
* Did sensitive files change?
* Did dependencies change?
* Did auth/payment/security/infra code change?
* Did final diff match the recorded receipt?
* Is the receipt valid?
* What evidence is high confidence?
* What evidence is missing or low confidence?
* What should the human reviewer focus on?

Example:

```text
AgentReceipt Review

Session: ar_ses_01J...
Detected provider: Codex CLI
Capture quality: high
Risk: medium

Summary:
- Modified 7 files
- Added 2 tests
- Ran npm test
- Changed auth/session logic
- No secret file changes detected
- No unknown network activity detected
- Final diff matches recorded receipt

Warnings:
1. Auth-sensitive files changed:
   - src/auth/session.ts
   - src/middleware/auth.ts

2. No typecheck command detected:
   Expected one of:
   - npm run typecheck
   - tsc --noEmit

Reviewer focus:
- Verify session expiry behavior.
- Check middleware fallback behavior.
- Confirm tests cover expired-token path.
```

## 10.9 `agentreceipt verify`

Verifies receipt integrity.

Checks:

* event hash chain
* receipt signature
* final diff hash
* manifest hash
* schema version
* trusted key if configured
* missing/corrupted artifacts

Example:

```text
Receipt valid.

Event chain: valid
Signature: valid
Final diff hash: valid
Signed by: local-dev-key
```

## 10.10 `agentreceipt export`

Exports receipt artifacts.

Supported MVP formats:

```bash
agentreceipt export --json
agentreceipt export --md
agentreceipt export --pr
```

Future:

```bash
agentreceipt export --pdf
```

## 11. Provider Integration

## 11.1 Codex CLI (MVP primary)

Codex support is the MVP-primary provider integration.

## 11.1.1 Why Codex first

Codex support provides strong `git + filesystem` evidence first with best-effort log enrichment. This fits the sidecar model:

```text
User runs Codex normally.
Filesystem and git signals are captured continuously.
Codex session logs are imported when available.
AgentReceipt records and reconciles them.
```

## 11.1.2 Codex evidence sources

### Source 1: Git monitor

Always high confidence.

Captures:

* start commit
* branch
* staged changes
* unstaged changes
* final diff
* diff snapshots
* final diff hash

### Source 2: Filesystem watcher

High confidence for writes.

Captures:

* modified files
* created files
* deleted files
* sensitive file changes
* dependency file changes
* CI/deployment file changes

### Source 3: Codex session logs

AgentReceipt should locate and parse local Codex session logs where available.

Likely locations:

```text
$CODEX_HOME/sessions/
$CODEX_HOME/archived_sessions/
~/.codex/sessions/
~/.codex/archived_sessions/
~/.codex/session_index.jsonl
```

Expected useful data may include:

* messages
* tool/function calls
* command outputs
* file change events
* compacted history
* session metadata

But interactive Codex session logs should be treated as non-contractual unless/until stable docs confirm the format.

Parser rules:

* stream JSONL line by line
* never assume schema stability
* preserve unknown event types as opaque provider events
* store parse warnings
* cap large payloads
* redact aggressively
* do not crash on malformed records
* expose confidence level clearly

### Source 4: Codex JSONL from non-interactive runs

Codex can emit structured JSONL in non-interactive execution modes.

AgentReceipt can support importing those logs, but this is not wrapped execution.

Example:

```bash
agentreceipt import codex-jsonl ./codex-run.jsonl
agentreceipt review --codex-jsonl ./codex-run.jsonl
```

This is useful for CI or users who already have structured logs.

## 11.1.3 Codex capture confidence

Expected:

```text
Git diff: high
Filesystem writes: high
Codex session logs: medium
Provider tool events: medium when present
File reads: low-medium
Bash commands: medium when present
Network calls: low
```

## 11.1.4 Codex success criteria

Codex MVP is successful when AgentReceipt can:

* record useful receipts during normal Codex use
* detect file changes
* generate diff timeline
* identify final diff
* import Codex session logs where available
* extract tool/command events when present
* mark capture confidence honestly
* produce a useful review even when provider logs are incomplete

## 11.2 Claude Code (deferred)

## 11.2.1 Claude evidence sources (deferred)

### Source 1: Hooks

AgentReceipt supports Claude hooks for:

* session start
* user prompt submission
* pre-tool use
* post-tool use
* permission request
* stop/session end
* MCP tool events where available

Hook handler:

```bash
agentreceipt ingest claude-hook
```

### Source 2: Claude transcripts

Used for reconciliation.

Purpose:

* fill missing context
* validate hook coverage
* recover session metadata
* enrich timeline
* compare hook events against transcript events

Important rule:

Hook parsing is secondary to the core `git + filesystem + event-chain` evidence model.

## 11.2.2 Claude capture confidence

Expected:

```text
Git diff: high
Filesystem writes: high
Claude tool events: high
Claude permission events: high
Claude transcript: high
Network calls: medium unless directly observed
```

## 11.2.3 Claude success criteria

Claude is successful when AgentReceipt can:

* ingest hook events live
* detect tool names
* detect file paths for read/write/edit events where available
* detect bash commands where available
* record permission events
* correlate tool events with file changes
* generate useful post-session review

## 12. Capture Architecture

## 12.1 Local sidecar process

`agentreceipt start` starts a local recording process.

Responsibilities:

* manage active session
* receive hook events
* write append-only event log
* monitor git state
* monitor filesystem writes
* reconcile provider logs
* expose live status
* finalize receipt

Long-term daemon socket:

```text
~/.agentreceipt/agentreceipt.sock
```

MVP can use a background process managed by `agentreceipt start`.

## 12.2 Event sources

MVP event sources:

```text
git_monitor
fs_watcher
codex_session_log
claude_hook
claude_transcript
manual_marker
receipt_finalizer
```

Later event sources:

```text
shell_hook
network_monitor
github_pr
cloud_sync
```

## 12.3 Filesystem watcher

Watches repo root.

Captures:

* file create
* file modify
* file delete
* rename where supported
* sensitive path changes
* dependency file changes
* generated large-file changes

Limitation:

Filesystem watchers capture writes, not reads.

Reads require:

* provider logs
* tool events
* shell instrumentation
* OS tracing

MVP should not claim universal read visibility.

## 12.4 Git monitor

The git monitor is the backbone of the product.

At session start:

```bash
git rev-parse --show-toplevel
git branch --show-current
git rev-parse HEAD
git status --porcelain=v1
git diff --binary
git diff --cached --binary
```

During session:

* snapshot after provider write/edit events
* snapshot after filesystem event bursts
* snapshot after command events
* debounce diff generation
* store patches by content hash

At session end:

* final branch
* final HEAD
* final staged diff
* final unstaged diff
* final changed file list
* final diff hash

## 12.5 Manual markers

Developers can add human context:

```bash
agentreceipt mark "Asked Claude to refactor session expiry logic"
agentreceipt mark "Manually reviewed generated auth changes"
agentreceipt mark "Approved dependency update"
```

Manual markers become signed events.

## 13. Receipt Data Model

## 13.1 Local storage layout

```text
~/.agentreceipt/
  repos/
    <repo-key>/
      sessions/
        ar_ses_01J.../
          events.jsonl
          receipt.json
          receipt.md
          review.md
          manifest.json
          diffs/
            000001.patch
            000002.patch
            final.patch
          provider/
            claude/
              hook-events.jsonl
              transcript.jsonl
              parse-report.json
            codex/
              imported-session.jsonl
              parse-report.json
          blobs/
            sha256-...
          signatures/
            receipt.sig
```

## 13.2 Event schema

```json
{
  "event_id": "evt_01J...",
  "session_id": "ar_ses_01J...",
  "seq": 42,
  "timestamp": "2026-06-16T12:00:00.000Z",
  "source": "codex_session_log",
  "type": "provider.tool.post_use",
  "provider": "codex",
  "cwd": "/repo",
  "payload": {
    "tool_name": "Edit",
    "file_path": "src/auth/session.ts",
    "success": true
  },
  "prev_hash": "sha256:...",
  "event_hash": "sha256:..."
}
```

## 13.3 Receipt summary schema

```json
{
  "schema_version": "0.2.0",
  "receipt_id": "ar_rcpt_01J...",
  "session_id": "ar_ses_01J...",
  "created_at": "2026-06-16T12:25:00Z",
  "mode": "sidecar",
  "agent": {
    "provider": "codex",
    "provider_confidence": "medium"
  },
  "repo": {
    "root": "/repo",
    "branch_start": "feature/auth",
    "branch_end": "feature/auth",
    "commit_start": "abc123",
    "commit_end": "abc123",
    "dirty_start": false,
    "dirty_end": true
  },
  "summary": {
    "duration_seconds": 1240,
    "files_changed": 7,
    "commands_detected": 4,
    "provider_tool_events": 31,
    "risk_events": 2,
    "tests_detected": ["npm test"],
    "final_diff_hash": "sha256:..."
  },
  "capture_confidence": {
    "git_diff": "high",
    "filesystem_writes": "high",
    "provider_tool_events": "high",
    "file_reads": "medium_high",
    "network_calls": "medium"
  },
  "risk": {
    "level": "medium",
    "reasons": [
      "Auth-sensitive files changed",
      "No typecheck command detected"
    ]
  },
  "verification": {
    "event_chain_hash": "sha256:...",
    "receipt_hash": "sha256:...",
    "signature_algorithm": "ed25519",
    "signature": "base64..."
  }
}
```

## 14. Verification Model

## 14.1 Hash chain

Each event includes:

* canonical event payload hash
* previous event hash
* sequence number
* timestamp

If someone modifies, deletes, or reorders events, verification fails.

## 14.2 Local signing

On first use:

```text
~/.agentreceipt/keys/default.ed25519
~/.agentreceipt/keys/default.pub
```

Final receipt signs:

* event chain hash
* final diff hash
* manifest hash
* receipt summary hash

## 14.3 Verification command

```bash
agentreceipt verify
```

Checks:

* event chain
* receipt hash
* signature
* final diff hash
* manifest
* provider parse reports
* trusted key status

Example:

```text
Receipt valid.

Event chain: valid
Signature: valid
Final diff hash: valid
Provider evidence: high confidence
```

## 15. Capture Confidence

Capture confidence must be visible in every review and receipt.

Example for Codex:

```text
Provider: Codex CLI
Capture quality: high

Evidence:
- Git diff captured
- Filesystem watcher active
- Codex session log parsed when available
- Some command/tool events extracted
```

Example for Claude:

```text
Provider: Claude Code
Capture quality: medium

Evidence:
- Claude hooks active
- Tool events captured
- Permission events captured
- Git diff captured
- Filesystem watcher active
- Transcript parsed

Limitations:
- Network activity may be incomplete
- Hook coverage may be partial
```

This should be treated as a product feature, not a disclaimer hidden in docs.

## 16. Risk Rules

## 16.1 Risk levels

```text
info
low
medium
high
critical
```

## 16.2 Default high-risk signals

High risk:

* `.env` changed
* private key file changed
* credentials file changed
* auth code changed
* payment code changed
* crypto/signing code changed
* CI/CD workflow changed
* destructive command detected
* dependency install detected
* package publishing command detected
* external network command detected
* final diff does not match receipt
* receipt verification fails

Medium risk:

* dependency file changed
* lockfile changed
* database migration changed
* Dockerfile changed
* deployment config changed
* no tests detected after code changes
* no lint/typecheck detected
* generated file too large
* Codex provider parse gaps

Low risk:

* docs-only change
* tests added
* tests run successfully
* no sensitive files touched
* receipt verified

## 16.3 Sensitive path defaults

```yaml
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
  - "src/security/**"
  - "src/payments/**"
  - "src/crypto/**"
  - "migrations/**"
  - "Dockerfile"
  - "docker-compose.yml"
```

## 16.4 Test command detection

```yaml
test_commands:
  - "npm test"
  - "npm run test"
  - "npm run lint"
  - "npm run typecheck"
  - "pnpm test"
  - "pnpm lint"
  - "pnpm typecheck"
  - "yarn test"
  - "cargo test"
  - "pytest"
  - "go test ./..."
  - "make test"
```

## 17. Policy Configuration

Optional explicit config file, passed with `--config`:

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
  - ".github/workflows/**"
  - "src/auth/**"
  - "src/payments/**"
  - "src/security/**"

test_commands:
  - "npm test"
  - "npm run lint"
  - "npm run typecheck"
```

## 18. Redaction and Privacy

AgentReceipt is local-first.

Default behavior:

* no cloud upload
* no account required
* no API key required
* raw prompts not exported by default
* raw tool outputs not exported by default
* raw provider logs stored locally only
* secrets redacted before export
* PR comments exclude sensitive payloads
* receipts include hashes instead of raw large outputs where possible

## 18.1 Redaction targets

Redact:

* API keys
* JWTs
* private keys
* SSH keys
* database URLs
* cloud credentials
* OAuth tokens
* wallet private keys
* `.env` values
* npm tokens
* PyPI tokens
* GitHub tokens
* Anthropic/OpenAI API keys

## 18.2 Raw provider logs

Raw provider logs can contain sensitive data.

Rules:

* store locally only by default
* never upload unless explicitly enabled
* never copy raw logs into PR comments
* parse into normalized low-sensitive events
* store large payloads by hash
* keep unknown records as opaque redacted blobs
* expose parse gaps in review

## 19. Session Review

Session review is the primary user-facing product.

## 19.1 Review report sections

`agentreceipt review` produces:

1. Session summary
2. Detected provider
3. Capture confidence
4. Verification status
5. Final diff summary
6. Files changed
7. Commands detected
8. Tests/lint/typecheck detected
9. Risk events
10. Timeline
11. Provider evidence used
12. Missing evidence / low-confidence areas
13. Suggested reviewer checklist

## 19.2 PR comment output

`agentreceipt review --pr` produces concise Markdown:

```md
## AgentReceipt

Status: Verified with warnings

Session:
- Provider: Codex CLI
- Duration: 18m 42s
- Files changed: 7
- Tool events: 31
- Commands detected: 4
- Tests detected: npm test

Risk:
- Medium: auth-sensitive files changed
- Medium: no typecheck command detected

Verification:
- Event chain: valid
- Signature: valid
- Final diff hash: valid

Capture confidence:
- Git diff: high
- Filesystem writes: high
- Claude tool events: high
- Network calls: medium

Reviewer focus:
- Verify session expiry behavior.
- Check middleware fallback behavior.
- Confirm tests cover expired-token path.
```

## 20. GitHub Workflow

The future CI-assisted and GitHub Check contract is specified in [GitHub PR Workflow Design](GITHUB_PR_WORKFLOW_DESIGN.md). The current product remains local-first and CLI-driven.

## 20.1 MVP: CLI-generated PR comment

Use GitHub CLI:

```bash
agentreceipt review --pr > agentreceipt-pr.md
gh pr comment --body-file agentreceipt-pr.md
```

Convenience command:

```bash
agentreceipt pr comment
```

## 20.2 Later: GitHub App

Hosted feature.

Capabilities:

* verify receipt on PR
* post PR comment
* set GitHub Check
* block missing/invalid receipt
* enforce team policy
* store history
* manage trusted keys

## 21. Competitive Strategy

## 21.1 What competitors validate

The existence of agent scoring and session-tracing products validates:

* developers care about agent trust
* teams are willing to pay for agent governance
* session data has commercial value
* Claude/Codex/Cursor workflows are important enough to monitor
* pilot packages are plausible early revenue

## 21.2 Where AgentReceipt should differ

AgentReceipt should be:

```text
local-first, not hosted-first
receipt-first, not score-first
PR-review-first, not dashboard-first
deterministic-first, not verifier-first
diff-specific, not agent-reputation-specific
independent observation, not agent self-reporting only
```

## 21.3 Main wedge

Competitor wedge:

```text
Compare agents and decide which one to trust.
```

AgentReceipt wedge:

```text
Verify the AI-generated diff in front of you before merge.
```

## 21.4 Features to avoid copying early

Avoid:

* agent trust profiles
* five-dimension scoring
* hosted AI verifier scoring
* workflow routing recommendations
* public agent comparisons
* reputation ledgers
* score dashboards

These may be useful much later, but they are not the MVP.

## 22. Business Model

## 22.1 Open-source local CLI

Free and open source.

Includes:

* sidecar recording
* Claude integration
* Codex integration
* local signed receipts
* local verification
* local review
* Markdown/JSON export
* basic risk rules
* GitHub PR comment generation

Rationale:

Developers will not trust a closed-source local recorder watching repos, provider logs, prompts, and diffs. Open source is part of the trust model.

## 22.2 Hosted Pro

Target:

* solo professionals
* indie developers
* consultants
* heavy Claude/Codex users

Pricing:

```text
$15/month
```

Features:

* hosted receipt history
* encrypted sync
* private receipt links
* search across receipts
* GitHub PR comments for personal/private repos
* 30-day retention
* richer web UI

## 22.3 Team

Target:

* startups
* engineering teams
* AI-heavy dev teams

Pricing:

```text
$25–40/developer/month
```

Features:

* GitHub org integration
* PR checks
* team policy rules
* receipt search
* team dashboard
* Slack alerts
* trusted signing keys
* retention controls
* private repo support

## 22.4 Enterprise self-hosted

Target:

* regulated teams
* security-sensitive companies
* enterprise engineering orgs

Pricing:

```text
$15k–50k/year initially
Custom for larger deployments
```

Features:

* self-hosted deployment
* SSO/SAML
* RBAC
* custom retention
* GitHub Enterprise/GitLab support
* private object storage
* custom redaction rules
* audit exports
* onboarding/support

## 22.5 Design partner package

Early revenue offer:

```text
AI Coding Agent Receipt Setup
$2k–5k early
$10k–25k after validation
```

Includes:

* install AgentReceipt in 1–3 repos
* configure Claude/Codex integrations
* define risk policy
* produce PR review workflow
* provide team onboarding
* collect product feedback

Goal:

Use services to learn and fund product development, not as the long-term business.

## 23. Technical Stack

## 23.1 Local CLI

Recommended MVP language:

```text
Go
```

Reasons:

* fast MVP iteration
* single-binary distribution
* good filesystem support
* good process support
* good crypto support
* easier than Rust for rapid product work

Alternative:

```text
Rust
```

Better for long-term trust/security positioning, but slower to ship.

Recommendation:

Use Go for MVP unless the brand strongly depends on Rust.

## 23.2 Local storage

Use:

* JSONL for append-only event log
* SQLite for local index/search
* patch files for diffs
* content-addressed blob store for large payloads
* Ed25519 signatures for receipts

## 23.3 Cloud later

Suggested stack:

* Next.js frontend
* TypeScript backend
* Postgres
* S3-compatible object storage
* GitHub App via Octokit/Probot
* WorkOS/Clerk for team auth later
* ClickHouse later only if event volume requires it

## 24. MVP Milestones

## Milestone 0: Provider research harness

Goal:

Validate evidence quality before full product polish.

Commands:

```bash
agentreceipt inspect codex --last
```

Questions answered:

* Where are logs?
* Are Codex logs updated during active sessions?
* Can Codex tool events be parsed?
* Can file paths be extracted?
* Can bash commands be extracted?
* Can session cwd/repo be detected?
* Can logs be correlated to git changes?
* What is the confidence level?

Deliverables:

* provider capability matrix
* parser fixtures
* sample receipts
* sample review outputs

## Milestone 1: Local sidecar skeleton

Commands:

```bash
agentreceipt init
agentreceipt start
agentreceipt status
agentreceipt stop
```

Features:

* session creation
* git start/end snapshot
* filesystem watcher
* event JSONL
* receipt JSON
* local signing

## Milestone 2: Codex MVP

Commands:

```bash
agentreceipt install codex
agentreceipt live
agentreceipt review --codex-jsonl ./codex-run.jsonl
```

Features:

* Codex home detection
* session log discovery
* streaming/chunked JSONL parser
* defensive schema handling
* git/fs correlation
* medium-confidence receipt generation

Success:

A normal Codex CLI session produces a useful receipt even if provider events are incomplete.

## Milestone 3: Claude MVP

Commands:

```bash
agentreceipt install claude
agentreceipt live
agentreceipt review
```

Features:

* Claude hook installer
* hook ingestion
* tool event normalization
* git diff correlation
* risk rules
* review report
* receipt verification

Success:

A Claude Code session produces a high-confidence receipt once hook coverage is stable.

## Milestone 4: PR workflow

Commands:

```bash
agentreceipt review --pr
agentreceipt pr comment
```

Features:

* PR Markdown generation
* GitHub CLI integration
* final diff verification
* concise reviewer checklist

## Milestone 5: Public alpha

Release:

* Homebrew install
* GitHub repo
* docs
* Claude demo
* Codex demo
* build-in-public posts
* example receipts
* example PR comments

## 25. Success Metrics

## Product metrics

* installs
* first receipt generated
* sessions recorded per user
* review command usage
* Claude integration activation
* Codex integration activation
* receipts attached to PRs
* verification failures caught
* risk warnings generated
* review usefulness feedback

## Business metrics

* GitHub stars
* waitlist signups
* design partner calls
* design partner revenue
* hosted beta signups
* paid conversion
* team activation

## Quality metrics

* Claude hook parse success rate
* Codex log parse success rate
* receipt verification reliability
* event-log corruption rate
* redaction false negatives
* false-positive risk warnings
* performance overhead

## 26. Build-in-Public Strategy

## 26.1 Core message

Every AI-generated PR should come with a receipt.

## 26.2 Content angles

* “The final diff is not enough.”
* “I’m building receipts for AI-generated PRs.”
* “Claude wrote the code. Here is the receipt.”
* “Codex changed 6 files. Here is what we can verify.”
* “AI code review needs evidence, not vibes.”
* “Use Claude normally. Get a signed audit trail.”
* “Use Codex normally. Review the session after.”
* “Agent scores are not receipts.”
* “Before you merge AI code, ask for the receipt.”

## 26.3 Demo sequence

Demo 1:

* Start AgentReceipt
* Run Claude normally
* Claude edits files
* AgentReceipt captures tool events
* Stop session
* Generate review

Demo 2:

* Run Codex normally
* AgentReceipt captures git/filesystem evidence
* Parses Codex logs where available
* Generates medium-confidence receipt

Demo 3:

* Agent changes auth code
* No tests run
* AgentReceipt flags reviewer checklist

Demo 4:

* Receipt modified manually
* Verification fails

Demo 5:

* Generate PR comment

## 27. Key Risks

## 27.1 Codex log stability

Risk:

Codex interactive logs may change or may not expose enough stable provider evidence.

Mitigation:

* treat Codex provider evidence as best-effort
* rely on git/filesystem evidence
* build parser fixtures
* version parsers
* expose confidence level
* avoid overpromising

## 27.2 Sensitive data exposure

Risk:

Provider logs may contain prompts, file contents, command output, secrets, and local paths.

Mitigation:

* local-first default
* aggressive redaction
* prompts off by default in exports
* raw logs local-only
* no cloud sync by default
* content-addressed blobs
* size caps

## 27.3 False sense of completeness

Risk:

Users may assume AgentReceipt saw everything.

Mitigation:

* capture confidence visible in every receipt
* explicit missing evidence section
* provider-specific confidence
* no universal hidden-action claims

## 27.4 Weak differentiation

Risk:

AgentReceipt may look like another agent monitoring/scoring tool.

Mitigation:

* avoid scoring language
* avoid comparison dashboards
* lead with PR receipts
* lead with local-first
* lead with signed diff verification
* compare against final merge decision, not agent selection

## 27.5 Review output too noisy

Risk:

Developers ignore long logs.

Mitigation:

* concise terminal review
* concise PR comment
* deterministic risk flags
* reviewer checklist
* detailed timeline only on demand

## 28. Final MVP Definition

The MVP is successful when a developer can:

1. Install AgentReceipt.
2. Run `agentreceipt start`.
3. Use Claude Code or Codex CLI normally.
4. Run `agentreceipt stop`.
5. Run `agentreceipt review`.
6. Get a useful signed receipt.
7. Attach a concise review artifact to a PR.
8. Verify the receipt locally.

No wrapper.
No orchestration.
No agent scoring.
No hosted dependency.

The MVP promise:

```text
Use Claude or Codex normally.
AgentReceipt gives the AI-generated diff a receipt.
```
