# Plan 007: Design GitHub PR check workflow

> **Executor instructions**: This is a design/spike plan. Do not build a GitHub App or CI enforcement in this plan. Produce the design and minimal tests/docs described below, then update `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat f9e7997..HEAD -- README.md docs/PRD.md docs/TECH_SPEC.md cmd/root.go .github/workflows`
> If these paths changed, compare current state against excerpts before proceeding.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: LOW
- **Depends on**: `plans/004-make-receipt-signatures-portable.md`
- **Category**: direction
- **Planned at**: commit `f9e7997`, 2026-06-17

## Why this matters

AgentReceipt already generates PR Markdown and can post it with `gh`, but team enforcement is explicitly not wired. The next useful team feature is a GitHub PR/check workflow that consumes signed local receipts without turning AgentReceipt into a hosted-first agent scoring product. A design plan prevents accidental scope creep into wrapping agents or uploading raw prompts.

## Current state

Current excerpts:

```md
README.md:29
- **Team enforcement is not wired yet.** GitHub App support, CI policy gates, configurable risk policy, and broader team workflow controls are planned follow-ups.
```

```go
// cmd/root.go:883
func newPRCommentCommand() *cobra.Command {
    return &cobra.Command{
        Use:   "comment",
        Short: "Post receipt Markdown to the current pull request",
        RunE: func(cmd *cobra.Command, _ []string) error {
            ...
            gh := exec.CommandContext(cmd.Context(), "gh", "pr", "comment", "--body-file", "-")
        },
    }
}
```

```yaml
# .github/workflows/ci.yml
on:
  push:
    branches:
      - main
  pull_request:
```

Repo conventions:
- PR support currently uses generated Markdown and `gh`, not a hosted service.
- Product docs reject agent scoring and hosted-first dashboards.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Docs check | `git diff --check -- README.md docs/TECH_SPEC.md docs/PRD.md` | no output |
| Command tests if touched | `go test ./cmd` | exit 0 |
| Full tests if code touched | `go test ./...` | exit 0 |

## Scope

**In scope**:
- New design doc such as `docs/GITHUB_PR_WORKFLOW_DESIGN.md`
- `docs/TECH_SPEC.md`
- `README.md` if limitations/roadmap wording needs a pointer
- `cmd/root.go` and tests only for documentation-aligned command wording

**Out of scope**:
- Creating a GitHub App.
- Adding CI gates that fail PRs.
- Uploading raw provider logs.
- Changing release workflows.

## Git workflow

- Branch: `advisor/007-github-pr-workflow-design`
- Commit message example: `docs: design GitHub PR workflow`.
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Define workflow variants

Document at least two variants:

- local-only: developer runs `agentreceipt review --pr` or `agentreceipt pr comment`;
- CI-assisted: CI verifies committed/uploaded receipt artifacts and reports a check.

For each, state inputs, outputs, trust boundaries, and what data leaves the machine.

**Verify**: `git diff --check -- docs/GITHUB_PR_WORKFLOW_DESIGN.md docs/TECH_SPEC.md` -> no output.

### Step 2: Define receipt artifact contract for CI

Specify the minimum artifacts a CI check would need:

- `receipt.json`;
- `events.jsonl`;
- `manifest.json`;
- `diffs/final.patch`;
- signature or embedded signer metadata from Plan 004.

Include expected failure modes: invalid event chain, diff mismatch, missing tests, sensitive path changes.

**Verify**: docs mention each artifact exactly in a clear contract section.

### Step 3: Define policy without scoring

Add a policy section that uses deterministic checks only, for example:

- require valid signature;
- require final diff match;
- warn or fail on sensitive path changes without tests;
- never compute an "agent trust score".

**Verify**: `rg -n "score|policy|signature|diff" docs/GITHUB_PR_WORKFLOW_DESIGN.md`.

### Step 4: Update roadmap wording

If useful, add one README link from "Team enforcement is not wired yet" to the new design doc. Do not claim implementation exists.

**Verify**:
- `git diff --check -- README.md docs/GITHUB_PR_WORKFLOW_DESIGN.md docs/TECH_SPEC.md` -> no output.
- `go test ./...` only if code changed.

## Test plan

- No runtime tests are required for docs-only work.
- If command help changes, add/update `cmd` tests.

## Done criteria

- [ ] Design doc defines local-only and CI-assisted variants.
- [ ] Artifact contract is explicit.
- [ ] Policy is deterministic and avoids agent scoring.
- [ ] README remains honest that enforcement is not wired.
- [ ] `git diff --check` passes.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- The design requires a hosted service decision the maintainer has not made.
- CI artifact upload would include raw prompts or raw provider logs by default.
- This plan starts turning into implementation.

## Maintenance notes

This design should be revisited after Plan 004, because portable signature metadata changes what CI can verify without local user keys.
