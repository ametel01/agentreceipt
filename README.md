# AgentReceipt

AgentReceipt is a local evidence sidecar for AI-assisted coding sessions. It records session activity, captures deterministic artifacts, and exposes machine-readable contracts for automated agent review loops.

## Why this workflow

`start` and `stop` capture runtime evidence. The machine contracts from that evidence are:

- `agentreceipt focus`: compact next-step work queue and control status.
- `agentreceipt replay`: factual timeline and evidence report.
- `agentreceipt schema`: JSON Schema for machine consumers.
- `agentreceipt verify diff`: patch-equivalence check between finalized patch and candidate.

Humans can still use `review`, `export`, and `verify`, but the agent-facing loop should prefer the JSON contracts first.

## Install AgentReceipt

Default binary install:

```bash
curl -fsSL https://ametel.dev/agentreceipt/install.sh | sh
```

Pin a specific version:

```bash
curl -fsSL https://ametel.dev/agentreceipt/install.sh | sh -s -- --version v0.10.0
```

Install binary and ask whether to install the skill (interactive prompt):

```bash
curl -fsSL https://ametel.dev/agentreceipt/install.sh | sh -s -- --version v0.10.0
# ... prompts: Install the AgentReceipt coding-agent skill? [Y/n]
```

Install binary and skill non-interactively:

```bash
curl -fsSL https://ametel.dev/agentreceipt/install.sh | sh -s -- --version v0.10.0 --install-skill
```

Install binary to a custom directory and install skill to a custom root:

```bash
curl -fsSL https://ametel.dev/agentreceipt/install.sh | sh -s -- --version v0.10.0 \
  --bin-dir "$HOME/tools/bin" --install-skill --skill-dir "$HOME/.config/agentreceipt-skills"
```

Skip skill installation:

```bash
curl -fsSL https://ametel.dev/agentreceipt/install.sh | sh -s -- --version v0.10.0 --no-install-skill
```

Environment variables:

```bash
AGENTRECEIPT_INSTALL_SKILL=1 sh scripts/install.sh   # install skill without prompt
AGENTRECEIPT_INSTALL_SKILL=1 AGENTRECEIPT_SKILL_DIR="$HOME/.config/agentreceipt-skills" sh scripts/install.sh
AGENTRECEIPT_INSTALL_SKILL=1 AGENTRECEIPT_INSTALL_VERSION=0.10.0 AGENTRECEIPT_INSTALL_DIR="$HOME/.local/bin" sh scripts/install.sh
```

Installed skill location:

- `AGENTRECEIPT_SKILL_DIR` if provided
- `--skill-dir <DIR>` if provided
- selected default skill root (`~/.agents/skills` or `~/.claude/skills`), with `~/.agents/skills` preferred when both exist

Installed skill final path is always:

```bash
<skills-root>/agentreceipt/SKILL.md
```

## Quick start (agent-facing)

```bash
agentreceipt init
agentreceipt start --watch
# run AI-assisted work
agentreceipt stop
agentreceipt sessions
agentreceipt focus --session <id>
agentreceipt verify diff --session <id> --against merge-base --json
```

## Core agent command set

### Primary loop commands

Use these first for autonomous loops and deterministic automation:

- `agentreceipt sessions`
- `agentreceipt focus --session <id>`
- `agentreceipt focus --replay replay.json`
- `agentreceipt replay --session <id>`
- `agentreceipt replay --session <id> --full`
- `agentreceipt replay --session <id> --events 80-120 --file path --evidence events.jsonl#seq=88`
- `agentreceipt schema replay`
- `agentreceipt schema focus`
- `agentreceipt verify diff --session <id> --against merge-base --json`

### Capture/setup commands (secondary)

- `agentreceipt init`
- `agentreceipt install codex`
- `agentreceipt install claude`
- `agentreceipt start`
- `agentreceipt start --watch`
- `agentreceipt stop`
- `agentreceipt events`
- `agentreceipt import codex-jsonl <path>`

### Human/inspection commands (secondary)

- `agentreceipt review`
- `agentreceipt review --json --session <id>`
- `agentreceipt export --json --session <id>`
- `agentreceipt export --md --session <id>`
- `agentreceipt verify`
- `agentreceipt verify bundle <path>`
- `agentreceipt mark "message"`
- `agentreceipt status`
- `agentreceipt inspect codex`
- `agentreceipt version`

## Suggested loop pattern

```bash
agentreceipt sessions
agentreceipt focus --session <id> > focus.json
status=$?

# process focus.json without treating non-zero as failure.
if [ "$status" -ne 0 ]; then
  # perform validation / retries before continuing
fi

agentreceipt replay --session <id> --events 80-120 --file <path>
agentreceipt verify diff --session <id> --against merge-base --json
```

`focus` and `replay` are contract-first outputs; parse stable fields such as `process_contract`, `reviewability`, `agent_tasks`, `recommended_next_commands`, `evidence_index`, and top-level outcome semantics.

## Install archive contents

Release archives now include both binary and installer skill artifact:

- `agentreceipt`
- `agentreceipt-skill/SKILL.md`

This keeps the installer source aligned with versioned CLI releases and avoids embedding large heredocs in `scripts/install.sh`.

## Known limitations

- Codex log parsing is best-effort and local-only; malformed or missing logs reduce provider confidence.
- AgentReceipt does not gate execution, sandbox, score models, upload prompts, or enforce team policy.
- Live watcher UX is currently Codex-first.
- Claude support is hook-based MVP and intentionally privacy-conservative.
- Raw prompts, raw tool output, and raw provider logs are excluded by default.
- Installer controls are non-destructive by default:
  - non-interactive overwrite of an existing differing skill is rejected
  - interactive mode prompts before overwrite

## Release notes and workflow

- `make verify` remains the release-gated quality check.
- Release assets are produced by `scripts/build-release-artifacts.sh` and consumed by `scripts/extract-release-notes.sh`.
- CI verifies Linux/macOS archives and publishes `SHA256SUMS` for release integrity checks.

For full design docs, see `README` sections in:

- `docs/replay-evaluator-contract.md`
- `docs/AI_AGENT_COMMAND_IMPROVEMENTS.md`
- `docs/task_description.md`
