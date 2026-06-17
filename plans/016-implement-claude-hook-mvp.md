# Plan 016: Implement the Claude hook MVP

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat c5ab6b4..HEAD -- docs/CLAUDE_PROVIDER_DESIGN.md internal/provider internal/session internal/review cmd/root.go cmd/root_test.go README.md`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: L
- **Risk**: HIGH
- **Depends on**: `plans/010-enforce-provider-privacy-contract.md`
- **Category**: direction
- **Planned at**: commit `c5ab6b4`, 2026-06-17

## Why this matters

AgentReceipt is positioned for Codex today and Claude later. The Claude provider design has been written, but the CLI still reports Claude hook installation as deferred. This plan turns the design into a minimal, auditable MVP while preserving the local-first sidecar model and the privacy contract.

## Current state

Relevant files:

- `docs/CLAUDE_PROVIDER_DESIGN.md` - provider-normalization and install contract.
- `cmd/root.go` - `install claude` currently prints deferred status.
- `internal/provider/codex` - existing provider parser pattern to imitate where applicable.
- `internal/session/session.go` - appends provider-neutral `model.Event` records.
- `internal/review/review.go` - consumes provider-neutral command and command-result events.

Current excerpts:

```go
// cmd/root.go:162
func newInstallClaudeCommand() *cobra.Command {
    return &cobra.Command{
        Use:   "claude",
        Short: "Show deferred Claude integration status",
        RunE: func(cmd *cobra.Command, _ []string) error {
            _, err := fmt.Fprintln(cmd.OutOrStdout(), "Claude hook installation is deferred in the Codex-first MVP; no runtime hooks were configured.")
            return err
        },
    }
}
```

```md
docs/CLAUDE_PROVIDER_DESIGN.md:19
Claude ingestion should normalize hook or transcript records into the existing `model.Event` shape so review logic stays provider-neutral.
```

Design constraints to honor:

- Git and filesystem evidence remain the high-confidence proof.
- Missing or malformed Claude evidence creates warnings and confidence downgrades, not finalization failure.
- AgentReceipt must not wrap, proxy, or control Claude execution.
- Prompt text should not be stored in normalized events by default.
- `install claude --dry-run` must show exact hook changes without writing.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Provider tests | `go test ./internal/provider/...` | exit 0 |
| Session/review tests | `go test ./internal/session ./internal/review` | exit 0 |
| CLI tests | `go test ./cmd` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Smoke | `./scripts/smoke.sh` | exit 0 |
| Final gate | `make verify` | exit 0 |

## Suggested executor toolkit

- Use the existing Codex parser tests as structural examples, but do not copy Codex-specific schema assumptions into Claude code.
- Read `docs/CLAUDE_PROVIDER_DESIGN.md` completely before editing.

## Scope

**In scope**:

- `internal/provider/claude` (create)
- `internal/provider/claude/*_test.go` (create)
- `cmd/root.go`
- `cmd/root_test.go`
- `internal/session/session.go` only if a provider append helper needs provider-neutral adjustment
- `internal/review/review.go` only for provider-neutral confidence/display gaps
- `README.md`
- `docs/CLAUDE_PROVIDER_DESIGN.md` only for implementation notes discovered during work

**Out of scope**:

- Wrapping Claude execution.
- Uploading hooks, transcripts, prompts, or tool outputs.
- Supporting Cursor, Aider, Gemini, IDE extensions, or hosted dashboards.
- GitHub App or CI enforcement.
- Full transcript import if hook ingestion is sufficient for MVP.

## Git workflow

- Branch: `advisor/016-claude-hook-mvp`
- Commit message example: `feat: add claude hook mvp`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Create provider package and normalized event model

Create `internal/provider/claude` with parser/normalizer functions that convert fixture hook records into `model.Event` records:

- command attempt -> `Type: "provider.command"`, `Provider: "claude"`
- command result -> `Type: "provider.command_result"`, `Provider: "claude"`
- opaque hook/tool lifecycle -> `Type: "provider.event"`, `Provider: "claude"`
- parse warning -> `model.Warning` with `claude_` prefix

Use synthetic fixtures based on the design contract. Do not embed real user transcripts.

**Verify**: `go test ./internal/provider/claude` -> exit 0.

### Step 2: Implement dry-run hook install planning

Change `agentreceipt install claude` to support `--dry-run` and explicit target path flags. Dry-run output must name every file it would create or modify and show the hook command it would install. It must not write files.

**Verify**: `go test ./cmd -run 'Claude|Install'` -> exit 0.

### Step 3: Implement guarded hook installation

Implement non-dry-run install with the contract from `docs/CLAUDE_PROVIDER_DESIGN.md`:

- merge with existing settings without deleting unrelated hooks;
- print every file created or modified;
- create backups or print a clear rollback path;
- validate that the hook command resolves to the current `agentreceipt` binary;
- never enable prompt/transcript retention by default.

Use temp-file and atomic-write patterns already present in config/storage code.

**Verify**: `go test ./cmd` -> exit 0.

### Step 4: Add hook ingestion command

Add the minimal command path needed for Claude hooks to append normalized events to an active session. It can be hidden/internal if intended only for Claude hook execution. It must:

- read one hook JSON record from stdin or a file;
- normalize via `internal/provider/claude`;
- append provider events through `session.Manager.AppendProviderEvents`;
- return non-zero on malformed input only when the hook contract requires it; otherwise emit a warning event.

**Verify**: `go test ./cmd ./internal/session ./internal/review` -> exit 0.

### Step 5: Update review/provider confidence where necessary

Review should remain provider-neutral. If `confidence` currently keys only on `codex_session_log`, adjust it to treat Claude provider evidence as provider tool evidence without reducing Codex behavior.

**Verify**: `go test ./internal/review` -> exit 0.

### Step 6: Update README limitations

Update README so it no longer says Claude hook installation is deferred once the command actually installs hooks. Keep limitations honest: note MVP hook coverage and privacy defaults.

**Verify**: `git diff --check -- README.md docs/CLAUDE_PROVIDER_DESIGN.md cmd/root.go` -> no output, exit 0.

### Step 7: Full validation

**Verify**:

- `go test ./...` -> exit 0.
- `./scripts/smoke.sh` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Provider parser tests for command attempt, command result, opaque event, malformed input, and privacy redaction.
- CLI dry-run test proving no files are written.
- CLI install test using a temp Claude settings path.
- Hook ingestion test appending to an active session and appearing in review.
- Review confidence test for Claude provider evidence.

## Done criteria

- [ ] `agentreceipt install claude --dry-run` shows exact hook changes without writing.
- [ ] `agentreceipt install claude` writes/merges hooks idempotently in a temp-tested settings path.
- [ ] Claude hook records normalize into provider-neutral session events.
- [ ] Review summaries show Claude command status through existing provider-neutral logic.
- [ ] Prompt/transcript/raw-output content is not stored by default.
- [ ] `go test ./...` exits 0.
- [ ] `make verify` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Claude hook payload fields are not stable enough to implement without real fixtures or docs.
- Implementing install would silently overwrite user Claude settings.
- Privacy Plan 010 has not landed or prompt/raw-output defaults are still untested.
- The work requires wrapping or proxying Claude execution.

## Maintenance notes

Keep Claude support provider-neutral. Review and receipt logic should not fork into parallel Codex and Claude implementations unless the evidence schema genuinely differs.
