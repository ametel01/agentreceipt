# Plan 003: Implement or remove inactive review flags

> **Executor instructions**: Follow this plan step by step. Run each verification command and confirm expected results. If a STOP condition occurs, stop and report. Update `plans/README.md` when done.
>
> **Drift check (run first)**: `git diff --stat f9e7997..HEAD -- cmd/root.go cmd/root_test.go internal/review/review.go internal/review/review_test.go README.md`
> If any in-scope file changed since this plan was written, compare current-state excerpts against live code.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: `plans/002-preserve-command-result-status-in-review.md`
- **Category**: dx
- **Planned at**: commit `f9e7997`, 2026-06-17

## Why this matters

The CLI exposes `review --full`, `review --codex-jsonl`, and `review --provider`, but those flags currently do not affect behavior. Inactive flags are costly in a review/provenance tool because users may believe they filtered or imported evidence when they did not. This plan either implements the flags fully or removes/deprecates them from the command surface and docs.

## Current state

Current excerpts:

```go
// cmd/root.go:679
reviewCmd.Flags().Bool("full", false, "Include expanded evidence details")
reviewCmd.Flags().String("codex-jsonl", "", "Import a Codex JSONL trace before building the review")
reviewCmd.Flags().String("provider", "", "Filter review output by provider")
```

```go
// cmd/root.go:967
func reviewOptionsFromCommand(cmd *cobra.Command) (review.Options, error) {
    // maps repo, session, last, security, diff only
    return review.Options{RepoPath: repoPath, SessionID: sessionID, Last: last, Security: security, Diff: diff}, nil
}
```

The README advertises:

```md
agentreceipt review --pr
agentreceipt review --json
agentreceipt review --md
```

Repo conventions:
- Command help is tested in `cmd/root_test.go`.
- Review rendering is deterministic and covered by `internal/review/review_test.go`.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Command tests | `go test ./cmd` | exit 0 |
| Review tests | `go test ./internal/review` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Markdown check | `git diff --check -- README.md cmd/root.go internal/review/review.go` | no output, exit 0 |

## Scope

**In scope**:
- `cmd/root.go`
- `cmd/root_test.go`
- `internal/review/review.go`
- `internal/review/review_test.go`
- `README.md` if user-facing command docs change

**Out of scope**:
- Adding a new provider integration.
- Reworking review output layout beyond the selected flag behavior.
- Changing receipt verification.

## Git workflow

- Branch: `advisor/003-review-flags`
- Commit message example: `Fix inactive review flags`.
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Choose implement or remove for each flag

Make a small decision table in a code comment or test name only if useful. Recommended decisions:

- `--codex-jsonl`: implement, because README/PRD mention importing a trace before review.
- `--provider`: remove unless there is immediate multi-provider behavior to filter.
- `--full`: implement only if it exposes already-available timeline/details; otherwise remove until a concrete full renderer exists.

Do not leave any flag registered without a test proving behavior.

**Verify**: `go test ./cmd -run TestReviewModeFlags` -> update expected flags and pass.

### Step 2: Implement `--codex-jsonl` or remove it cleanly

If implementing:

- Add fields to `review.Options` for `CodexJSONL` as needed.
- Before building the report, parse/import the specified trace similarly to `import codex-jsonl`.
- Ensure behavior is explicit when no active session exists.

If removing:

- Delete the flag from `cmd/root.go`.
- Remove docs or tests that imply it exists.

**Verify**: `go test ./cmd ./internal/review` -> exit 0.

### Step 3: Handle `--provider` and `--full`

For each flag, either implement behavior and tests or remove it from the command surface. Implementation must be user-visible and deterministic:

- `--provider codex` should filter provider-derived evidence or reject unsupported providers.
- `--full` should add expanded timeline/evidence sections in terminal/Markdown/JSON as appropriate.

**Verify**: add tests that fail without the implemented behavior or fail if removed flags still appear in help.

### Step 4: Update README if command surface changes

If flags are removed or behavior is added, update the nearest README command examples. Do not touch unrelated docs.

**Verify**:
- `git diff --check -- README.md cmd/root.go internal/review/review.go` -> no output.
- `go test ./...` -> exit 0.

## Test plan

- Update `TestReviewModeFlags` so registered flags match actual behavior.
- Add behavior tests for any implemented flag.
- Add one negative test for unsupported provider if `--provider` remains.

## Done criteria

- [ ] No inactive review flag remains.
- [ ] Help output and README match implemented behavior.
- [ ] `go test ./cmd ./internal/review` exits 0.
- [ ] `go test ./...` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Implementing `--codex-jsonl` requires changing active session lifecycle from Plan 001.
- Supporting `--provider` requires a provider abstraction not present in current code.
- Product owner wants to keep a roadmap flag visible despite no behavior.

## Maintenance notes

New CLI flags should be added with tests that prove behavior, not only help registration.
