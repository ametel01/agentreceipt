# Plan 006: Design Claude hook support before implementation

> **Executor instructions**: This is a design/spike plan, not a build-all plan. Follow the steps, update docs/tests only where specified, and stop at open questions instead of inventing behavior. Update `plans/README.md` when done.
>
> **Drift check (run first)**: `git diff --stat f9e7997..HEAD -- README.md docs/PRD.md docs/TECH_SPEC.md cmd/root.go internal/provider`
> If these files changed, compare the current state below against live code.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW
- **Depends on**: `plans/004-make-receipt-signatures-portable.md`
- **Category**: direction
- **Planned at**: commit `f9e7997`, 2026-06-17

## Why this matters

The README now positions AgentReceipt for users running Codex today and Claude once hook support lands. The current `agentreceipt install claude` command correctly reports that hook integration is deferred. Before implementation, the repo needs a precise design for Claude event ingestion, confidence labels, storage paths, and parity with Codex review semantics.

## Current state

Current excerpts:

```md
README.md:5
It is built for developers who run agents in permissive "YOLO mode": Codex today, and Claude once hook support lands.
```

```go
// cmd/root.go:185
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

```go
// internal/storage/storage.go reserves paths
ProviderClaude       string
ClaudeParseReport    string
```

Repo conventions:
- Provider evidence is enrichment, not the core proof mechanism.
- Missing provider evidence should downgrade confidence, not block finalization.
- Storage already has `provider/claude` reserved.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Docs check | `git diff --check -- README.md docs/TECH_SPEC.md docs/PRD.md` | no output |
| Command tests if touched | `go test ./cmd` | exit 0 |
| Full tests if code touched | `go test ./...` | exit 0 |

## Scope

**In scope**:
- `docs/TECH_SPEC.md`
- `docs/PRD.md` only if product commitments need clarification
- `README.md` only if user-facing limitations change
- Optional new design doc under `docs/`, such as `docs/CLAUDE_PROVIDER_DESIGN.md`
- `cmd/root.go` and `cmd/root_test.go` only for wording/tests if install command messaging changes

**Out of scope**:
- Installing Claude hooks.
- Writing a Claude parser.
- Changing receipt schema beyond design notes.

## Git workflow

- Branch: `advisor/006-claude-provider-design`
- Commit message example: `docs: design Claude provider support`.
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Document event model parity

Create or update a docs section that defines Claude provider event normalization in terms already used by Codex:

- provider command attempt;
- provider command result;
- file/tool event;
- parse warning;
- confidence record.

Explicitly state which fields must map into existing `model.Event` payloads so review logic can stay provider-neutral.

**Verify**: `git diff --check -- docs/TECH_SPEC.md docs/CLAUDE_PROVIDER_DESIGN.md` -> no output.

### Step 2: Define storage and privacy behavior

Document how Claude artifacts use existing storage:

```text
provider/claude/
  parse-report.json
  hook-events.jsonl
  transcript.jsonl
```

State redaction rules and whether raw hooks/transcripts are stored by default. Match the existing privacy posture: no prompt upload, local-first, raw provider logs local only.

**Verify**: search docs for contradictions: `rg -n "Claude|provider/claude|raw provider" README.md docs`.

### Step 3: Define install command contract

Decide what `agentreceipt install claude` should eventually do and what it must not do:

- must not silently mutate user shell or Claude config without clear output;
- should have dry-run or explicit path behavior if hooks are installed;
- should report exactly which files were changed.

If current deferred output remains correct, leave code unchanged.

**Verify**: `go test ./cmd -run Claude` if command tests exist or are added; otherwise `go test ./cmd`.

### Step 4: List implementation questions and acceptance criteria

Add a short "Open questions" and "MVP acceptance criteria" section. Include STOP-worthy unknowns, such as unstable Claude hook payload shape or permission prompts.

**Verify**:
- `git diff --check -- README.md docs/TECH_SPEC.md docs/PRD.md docs/CLAUDE_PROVIDER_DESIGN.md` -> no output.
- `go test ./...` if any code changed.

## Test plan

- No runtime tests are required if only docs change.
- If command wording changes, update command tests to assert the new text.

## Done criteria

- [ ] Claude provider design exists and is self-contained.
- [ ] Storage, privacy, confidence, and event normalization are specified.
- [ ] Current README limitations remain accurate.
- [ ] `git diff --check` passes for touched docs.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Claude hook format cannot be determined from reliable local docs or experiments.
- The design requires behavior that contradicts the sidecar/no-wrapper product decision.
- Implementation appears small enough to tempt coding it in this plan; do not implement.

## Maintenance notes

The design should make the later implementation boring: one parser, one importer, one install/update command, and provider-neutral review summaries.
