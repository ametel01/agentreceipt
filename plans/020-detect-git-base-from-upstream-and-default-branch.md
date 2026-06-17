# Plan 020: Detect git review bases from upstream and default branch metadata

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 28911ab..HEAD -- internal/review/review.go internal/review/review_test.go cmd/root.go cmd/root_test.go`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `28911ab`, 2026-06-17

## Why this matters

AgentReceipt review output is meant to summarize branch state and reviewer-visible diffs for arbitrary repositories. Today it only looks for `main`, `master`, `origin/main`, and `origin/master`. Repositories whose default branch is `develop`, `trunk`, or a configured upstream branch lose base detection, ahead/behind counts, and branch diff summaries even when git has enough metadata to compute them.

## Current state

Relevant files:

- `internal/review/review.go` - git branch/base/diff summary helpers.
- `internal/review/review_test.go` - git-summary tests currently cover only `main`.
- `cmd/root.go` and `cmd/root_test.go` - only in scope if CLI review output assertions need updates.

Current excerpts:

```go
// internal/review/review.go:317
func detectBaseRef(ctx context.Context, repoRoot string) (string, bool) {
    for _, candidate := range []string{"main", "master", "origin/main", "origin/master"} {
        if _, err := gitVerifyBase(ctx, repoRoot, candidate); err == nil {
            return candidate, true
        }
    }

    return "", false
}
```

```go
// internal/review/review.go:597
func gitVerifyBase(ctx context.Context, dir string, base string) (string, error) {
    var cmd *exec.Cmd
    switch base {
    case "main":
        cmd = exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", "main^{commit}")
    case "master":
        cmd = exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", "master^{commit}")
    case "origin/main":
        cmd = exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", "origin/main^{commit}")
    case "origin/master":
        cmd = exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", "origin/master^{commit}")
    default:
        return "", fmt.Errorf("unsupported base ref %q", base)
    }
    cmd.Dir = dir

    return gitCommandOutput(cmd, "git rev-parse --verify --quiet "+base+"^{commit}")
}
```

```go
// internal/review/review_test.go:96
if report.Git.Branch != "feature/review-diff" || report.Git.Base != "main" || !report.Git.BaseFound {
    t.Fatalf("unexpected git branch summary: %+v", report.Git)
}
```

Repo conventions:

- Git commands are constructed with fixed argument arrays, not shell strings.
- Error messages include the git command description and combined output.
- Tests create temp git repositories with helper functions; use that pattern.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Review tests | `go test ./internal/review` | exit 0 |
| CLI tests | `go test ./cmd` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Final gate | `make verify` | exit 0; note that it writes `coverage.out` and `./agentreceipt` |

## Scope

**In scope**:

- `internal/review/review.go`
- `internal/review/review_test.go`
- `cmd/root.go` only if render text needs a base-label adjustment
- `cmd/root_test.go` only if CLI review tests need fixture updates

**Out of scope**:

- Fetching from remotes.
- Contacting GitHub or using `gh`.
- Changing receipt final diff hashing.
- Rewriting git monitor capture logic.
- Supporting arbitrary user-provided base flags unless the operator explicitly asks for that product surface.

## Git workflow

- Branch: `advisor/020-git-base-detection`
- Commit message example from repo style: `fix: detect git review base from upstream`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add tests for non-main default branch and upstream branch

In `internal/review/review_test.go`, add focused tests using temp repositories:

1. A repository whose initial branch is renamed to `trunk`, then a `feature/review-diff` branch is created. Build a review and assert `BaseFound == true`, `Base == "trunk"`, and branch diff counts are non-zero.
2. If practical without network, create a local bare repo as `origin`, push a `develop` branch, configure the feature branch upstream to `origin/develop`, and assert the detected base prefers the configured upstream.

Keep fixtures local. Do not use real remotes.

**Verify**: `go test ./internal/review -run 'Base|Branch|Git'` -> the new non-main test should fail before implementation.

### Step 2: Replace switch-only git helpers with validated ref arguments

Refactor git helpers so they can accept validated refs instead of only four hard-coded strings. Keep safety by:

- never invoking a shell;
- validating base refs with `git rev-parse --verify --quiet <ref>^{commit}`;
- passing ref arguments as separate `exec.CommandContext` arguments;
- rejecting empty refs and refs containing whitespace or shell metacharacters if you add local validation.

The resulting helpers should support at least:

- `@{upstream}` when configured;
- `origin/HEAD` resolved to its target if present;
- local default branches beyond `main` and `master`;
- existing `main`, `master`, `origin/main`, `origin/master` behavior.

**Verify**: `go test ./internal/review -run 'Base|Branch|Git'` -> exit 0.

### Step 3: Implement base candidate ordering

Use a deterministic candidate order. Recommended:

1. current branch upstream (`@{upstream}`), if it resolves;
2. `origin/HEAD` resolved with `git symbolic-ref --quiet --short refs/remotes/origin/HEAD`, if available;
3. local branches from likely names: `main`, `master`, `trunk`, `develop`;
4. remote branches from likely names: `origin/main`, `origin/master`, `origin/trunk`, `origin/develop`.

Record the human-readable base string in `GitSummary.Base`. Do not fetch or create remote refs.

**Verify**: `go test ./internal/review` -> exit 0.

### Step 4: Preserve render behavior

Review terminal and Markdown rendering should keep showing:

- base name when found;
- "not found (looked for ...)" when no candidate exists.

Update the "looked for main/master" text if it becomes inaccurate. Keep output concise.

**Verify**: `go test ./cmd ./internal/review` -> exit 0.

### Step 5: Full validation

**Verify**:

- `go test ./...` -> exit 0.
- `go vet ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Existing `main` branch review test remains green.
- New local `trunk` or `develop` default branch test passes.
- New upstream branch test passes if added.
- Base-not-found behavior remains covered or manually verified by a small unit test.

## Done criteria

- [ ] Review detects a non-main local default branch such as `trunk` or `develop`.
- [ ] Review uses a configured upstream branch when available.
- [ ] Review still detects `main`, `master`, `origin/main`, and `origin/master`.
- [ ] No git command is executed through a shell.
- [ ] `go test ./...` exits 0.
- [ ] `go vet ./...` exits 0.
- [ ] `make verify` exits 0, or the operator records why the mutating gate was skipped.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Supporting upstream/default detection requires network fetches.
- Local git versions in CI do not support a command chosen by the implementation.
- Ref validation becomes complex enough to need a separate design decision.
- The fix requires changing git monitor or receipt hashing logic.

## Maintenance notes

Reviewers should inspect git command construction carefully. The desired behavior is more flexible ref detection without introducing shell injection risk or remote network activity.
