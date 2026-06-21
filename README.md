# AgentReceipt

AgentReceipt is a **local-first CLI for watching AI coding sessions as they happen**.

It is built for developers who run agents in permissive "YOLO mode": Codex live watching today, plus a Claude hook MVP for local provider evidence. Keep your normal terminal workflow, run AgentReceipt beside the agent, and see tool calls, commands, edits, token usage, warnings, and final review evidence without wrapping or proxying the agent.

Install the latest release:

```bash
curl -fsSL https://ametel.dev/agentreceipt/install.sh | sh
```

Pin a specific release:

```bash
curl -fsSL https://ametel.dev/agentreceipt/install.sh | sh -s -- --version v0.7.0
```

```bash
agentreceipt start --watch
```

While it watches, AgentReceipt records observable local evidence from your workspace. When the session is done, it turns that evidence into a verifiable receipt and reviewer-focused summary for PRs.

## Current limitations

- **Live watch is Codex-first.** `start --watch`, `inspect codex`, and `import codex-jsonl` work with Codex logs today. `agentreceipt install claude` installs an MVP Claude hook target, but Claude coverage is hook-driven rather than a full live watch/transcript importer.
- **Claude privacy defaults are conservative.** Claude hook ingestion stores normalized command/tool metadata by default. Prompt text, transcripts, and raw tool output are not retained unless config explicitly opts in.
- **Codex log parsing is best effort.** Interactive Codex logs are treated as local evidence, not a stable provider API. Missing, incomplete, malformed, or format-changed logs reduce provider confidence but do not stop receipt generation.
- **AgentReceipt observes; it does not gate.** It does not launch, wrap, sandbox, approve, deny, or proxy agent actions. This is intentional for the sidecar model, but permission enforcement is outside the current release.
- **Risk classification is heuristic.** Command risk badges use built-in rules for high/medium/low signals. They help reviewers focus attention, but they are not a policy engine and do not replace review.
- **Team enforcement is not wired yet.** GitHub App support, CI policy gates, hosted policy distribution, and broader team workflow controls are planned follow-ups; see [GitHub PR Workflow Design](docs/GITHUB_PR_WORKFLOW_DESIGN.md).

## Why this exists

AI-assisted coding is hard to review because most of the important activity happens before the final diff. AgentReceipt gives you a live window into the current session and leaves behind evidence you can verify later.

The watch view answers the immediate questions:

- What command or tool did the agent just run?
- Did the command pass or fail?
- How many tokens did that action use, and what is the session total?
- Which Codex log is being followed?
- Did parsing or evidence capture produce warnings?

The final review answers the merge-time questions:

- Which files changed and how?
- Did the session include risky paths (auth, security, deployments)?
- Were tests/lint/typecheck commands detected?
- Did the final diff match what was observed during the session?
- Did we lose evidence because logs were incomplete?

## What is included in this MVP

AgentReceipt MVP focuses on:

- **Codex-first support** as the primary live provider path
- Claude hook installation and active-session hook ingestion as an MVP provider path
- live `start --watch` visibility for the active Codex session
- compact command/edit/token/warning output designed for repeated terminal use
- explicit session recording with `start` / `stop`
- `git` + filesystem evidence as high-confidence sources
- best-effort Codex session-log enrichment (non-blocking)
- hash-chained event log
- receipt signing (Ed25519)
- local review + verification + export
- concise, no-noise output designed for code review

Planned for later:

- broader Claude transcript/import coverage and richer hook schemas
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

### 2) Watch the current session

```bash
agentreceipt start --watch
```

`--watch` starts an AgentReceipt session, follows the matching Codex JSONL session log for the current repository, prints command/edit/token/warning events as they appear, and imports those provider events into the active receipt.

```bash
Started AgentReceipt session ar_ses_...
Watching Codex logs. Press Ctrl-C to stop watching; the AgentReceipt session stays active until `agentreceipt stop`.
codex  watch   rollout-2026-06-17T02-53-28.jsonl (cwd /path/to/repo)
codex  ok      run make test (exit 0)
codex  tokens  361 (208899 session) after run make test
codex  ok      edit apply_patch (exit 0)
```

Press `Ctrl-C` to stop watching; the receipt session remains active until you run `agentreceipt stop`.

You can also record without foreground watch output:

```bash
agentreceipt start
```

Watch and review output are human-readable by default. Watch output is backed by structured watch events so later machine-readable rendering can reuse the same event shape. Color is controlled with `--color auto|always|never`; `auto` enables color only for terminal output.

AgentReceipt keeps streaming logs and report rendering separate. `start --watch` uses `zerolog` for structured one-line runtime events. Review, receipt, verify, Markdown, PR, and short Cobra command responses use explicit renderers so their layout stays deterministic and reviewer-focused.

Useful watch options:

```bash
agentreceipt start --watch --watch-interval 500ms
agentreceipt start --watch --watch-existing
agentreceipt start --watch --codex-home ~/.codex
agentreceipt --color never start --watch
agentreceipt --color always start --watch
```

When multiple Codex sessions exist, AgentReceipt prefers logs whose Codex `cwd` metadata matches the current git repository. Newly created Codex logs without `cwd` metadata are followed briefly so early tool calls are not missed.

### 3) Find recorded sessions

```bash
agentreceipt sessions
```

`sessions` lists AgentReceipt sessions for the current repository, newest first. Use it when you need a session ID for `review --session`, `verify --session`, `replay --session`, or `export --session`.

```text
SESSION                                  STATE      ACTIVE  UPDATED              EVENTS  WARNINGS
ar_ses_...                               active     *       2026-06-18 03:06:15  450     0
ar_ses_...                               finalized          2026-06-17 16:37:57  7141    0
```

The `ACTIVE` column marks the current open session with `*`.

### 4) End session and finalize

```bash
agentreceipt stop
```

### 5) Review the receipt

```bash
agentreceipt review
```

### 6) Verify integrity

```bash
agentreceipt verify
agentreceipt verify bundle ./agentreceipt
```

Receipts embed the signer public key and key ID, so verification works from shared artifacts without the signer's local key directory.
`verify bundle` checks a local artifact bundle and does not contact GitHub or enforce CI policy.

### 7) Replay for verifier workflows

```bash
agentreceipt replay --session <id>
agentreceipt replay --session <id> --json
agentreceipt replay --session <id> --bundle ./replay-bundle
```

`replay` builds a verifier-facing JSON report from stored session artifacts.

- `--session <id>` is required; there is no implicit latest/active-session replay.
- Output is machine-readable JSON with `kind: "agentreceipt.session_replay"`.
- Replay is a factual evidence surface: it reports sequence events, commands, file evidence, integrity checks, and gaps, but it does not score the session or apply policy.
- `--bundle <path>` writes a portable replay bundle containing:
  - `replay.json`
  - `receipt.json`
  - `manifest.json`
  - `events.jsonl`
  - `diffs/final.patch`
  - optional normalized provider traces from `provider/codex/traces/`
- Replay is artifact-only: no command reruns, no patch application, no model calls, no workspace mutation.
- Raw prompts, raw tool output, and raw provider logs are not included by default.
- Evaluator conclusions should be inferred from command/output evidence and integrity status in `replay.json`, not by any built-in policy rule in this command.
- Rebuild the CLI (`go build -o agentreceipt .`) before checking replay output for behavior changes to ensure you are reading the latest source code.

Replay JSON now includes explicit contract fields for verification, quality gates, patch summary,
policy checks, review focus, privacy, claims, and final outcome. The detailed contract is documented
in [docs/replay-evaluator-contract.md](docs/replay-evaluator-contract.md).

### 8) Export for PRs

```bash
agentreceipt review --pr
agentreceipt review --json
agentreceipt review --md
agentreceipt export --md
agentreceipt export --json
agentreceipt export --pr
agentreceipt pr comment
```

## Release notes extraction

CI release jobs can extract the GitHub Release body for a specific SemVer section from `CHANGELOG.md`:

```bash
scripts/extract-release-notes.sh --unreleased CHANGELOG.md > release-notes.md
scripts/extract-release-notes.sh --version 0.1.0 --changelog CHANGELOG.md > release-notes.md
scripts/extract-release-notes.sh v0.1.0 CHANGELOG.md > release-notes.md
```

The script accepts `--unreleased` for pre-release checks, and released versions with or without a leading `v`. It extracts only that release section body and fails if the requested section is missing or empty.

## Release workflow

Pushing a `v*` tag runs the release workflow. The workflow verifies the repo with `make verify`, extracts the matching SemVer section from `CHANGELOG.md`, builds Linux and macOS archives for `amd64` and `arm64`, writes `SHA256SUMS`, and publishes those assets to the GitHub Release.

Before tagging, move the relevant `Unreleased` entries into a matching release section, for example `## [0.1.0] - 2026-06-17`. The release workflow fails if that section is missing or empty.

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

- branch/base state against `main`, `master`, `origin/main`, or `origin/master`
- ahead/behind counts for the branch
- Git diff stats for branch changes and current workspace changes
- staged, unstaged, and untracked working-tree counts
- whether the finalized receipt diff still matches the current workspace
- detected commands (tests/lint/typecheck when available)
- tool calls the agent attempted (edit/read/write, command, tests/lint/typecheck)
- whether sensitive paths changed
- whether external risks were detected
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
- replay output is redacted and excludes raw prompts, raw command output, and raw provider logs by default

Explicit `--config` files control local review policy, including configured quality commands, test/typecheck prompts, and dependency/auth/secret-path risk flags.

## Quick command reference

The visible CLI surface is:

| Command | Purpose |
| --- | --- |
| `agentreceipt init` | Bootstrap global AgentReceipt storage and signing keys. |
| `agentreceipt install codex` | Detect local Codex log availability. |
| `agentreceipt install claude` | Install or update the Claude Code hook integration. |
| `agentreceipt start` | Start a local receipt capture session. |
| `agentreceipt start --watch` | Start or resume a session while tailing matching Codex logs. |
| `agentreceipt status` | Show active session health and event counts. |
| `agentreceipt sessions` | List sessions available for the current repository. |
| `agentreceipt events` | Show recent session events as a readable timeline. |
| `agentreceipt stop` | Finalize the active capture session. |
| `agentreceipt review` | Build a reviewer-focused receipt summary. |
| `agentreceipt verify` | Verify receipt integrity and signatures. |
| `agentreceipt verify bundle <path>` | Verify a local AgentReceipt artifact bundle. |
| `agentreceipt replay` | Build a machine-readable verifier replay report from a specific session and optionally write a portable replay bundle. Replay reports evidence facts only; no session scoring or policy decisions are made. |
| `agentreceipt export` | Export finalized receipt artifacts. |
| `agentreceipt import codex-jsonl <path>` | Import a Codex JSONL trace into the active session. |
| `agentreceipt inspect codex` | Inspect local Codex evidence availability. |
| `agentreceipt mark <message>` | Add a human context marker to the active session. |
| `agentreceipt pr comment` | Post receipt Markdown to the current pull request. |
| `agentreceipt version` | Print the AgentReceipt version. |
| `agentreceipt completion <shell>` | Generate shell completion for `bash`, `fish`, `powershell`, or `zsh`. |
| `agentreceipt help [command]` | Show command help. |

The deprecated hidden `agentreceipt live` alias remains for compatibility, but `agentreceipt events` is the documented command.

```bash
# Setup
agentreceipt init # optional global storage/key setup
agentreceipt install codex # optional read-only Codex log detection

# Session lifecycle
agentreceipt start
agentreceipt start --watch
agentreceipt status
agentreceipt sessions
agentreceipt events
agentreceipt events --format json # indented JSON array
agentreceipt events --format jsonl # compact JSON lines for scripts
agentreceipt stop

# Review & checks
agentreceipt review
agentreceipt review --last
agentreceipt review --session <id>
agentreceipt review --security
agentreceipt review --diff
agentreceipt review --codex-jsonl ./codex-run.jsonl
agentreceipt review --json
agentreceipt review --md
agentreceipt review --pr
agentreceipt verify
agentreceipt verify --session <id>
agentreceipt verify bundle ./agentreceipt
agentreceipt replay --session <id>
agentreceipt replay --session <id> --bundle ./replay-bundle

# Exports
agentreceipt export --json
agentreceipt export --md
agentreceipt export --pr
agentreceipt export --session <id> --json

# Parsers
agentreceipt inspect codex --last
agentreceipt import codex-jsonl ./codex-run.jsonl
agentreceipt install claude --dry-run --settings ~/.claude/settings.json

# Human context and PRs
agentreceipt mark "Manually reviewed generated auth changes"
agentreceipt pr comment

# Utility
agentreceipt version
agentreceipt completion zsh
agentreceipt help events
```

> Note: Codex remains the primary live-watch path. Claude support currently installs a local hook command that imports normalized hook JSON into the active session without retaining prompts or raw tool output by default.

## Example review output (what to look for)

```text
AgentReceipt Review

Session: ar_ses_01J...
Provider: Codex CLI
State: finalized
Risk: medium

Branch state:
- Branch: feature/auth-review
- Base: main
- Ahead/behind: 3 ahead, 0 behind
- Working tree: dirty (0 staged, 1 unstaged, 0 untracked)
- Receipt diff: matches current workspace

Diff:
- Branch vs main: 7 files changed, 122 insertions(+), 34 deletions(-)
  cmd/root.go | 42 +++++++++++++++++------
  internal/review/review.go | 71 +++++++++++++++++++++++++++++---------
- Workspace vs HEAD: 1 file changed, 8 insertions(+), 2 deletions(-)
  Makefile | 10 ++++++++--

Session evidence:
- Commands detected: 6
- Filesystem write events: 4 files
- Provider tool events: 18

Warnings:
- none
```

## Where this fits in your workflow

Use it as a deterministic review step:

1. Run a normal AI session.
2. Stop and review the receipt.
3. Attach the generated review artifact to your PR.
4. Verify before merge if required by your team.

This is not a "model score." It is a **receipts-first** check: evidence, integrity, and review context.

## License

AgentReceipt is licensed under the Apache License 2.0. See [LICENSE](LICENSE).
