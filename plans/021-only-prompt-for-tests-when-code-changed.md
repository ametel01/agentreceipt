# Plan 021: Only prompt for tests when code changed

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 28911ab..HEAD -- internal/review/review.go internal/review/review_test.go internal/model/model.go internal/capture/fswatcher/fswatcher.go`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: dx
- **Planned at**: commit `28911ab`, 2026-06-17

## Why this matters

Review output is meant to be concise and reviewer-focused. Today `Confirm appropriate tests were run for code changes` and `No test command detected` are emitted whenever no test command was observed, even if the changed files are docs, comments, or other non-code artifacts. That creates noise in the primary review surface and trains reviewers to ignore gaps that should remain meaningful.

## Current state

Relevant files:

- `internal/review/review.go` - builds focus and gap prompts.
- `internal/review/review_test.go` - policy toggle tests.
- `internal/model/model.go` - changed-file summary type.
- `internal/capture/fswatcher/fswatcher.go` - classifies changed paths as sensitive/dependency but not code.

Current excerpts:

```go
// internal/review/review.go:1114
if cfg.Review.RequireTestsForCodeChanges && !summary.TestDetected {
    items = append(items, "Confirm appropriate tests were run for code changes.")
}
if cfg.Review.RequireTypecheckForTS && hasTypeScriptChanges(summary) && !summary.TypecheckDetected {
    items = append(items, "Confirm typecheck coverage where relevant.")
}
```

```go
// internal/review/review.go:1129
if cfg.Review.RequireTestsForCodeChanges && !summary.TestDetected {
    gaps = append(gaps, "No test command detected.")
}
if !summary.LintDetected {
    gaps = append(gaps, "No lint command detected.")
}
if cfg.Review.RequireTypecheckForTS && hasTypeScriptChanges(summary) && !summary.TypecheckDetected {
    gaps = append(gaps, "No typecheck command detected for TypeScript changes.")
}
```

```go
// internal/review/review.go:833
func hasTypeScriptChanges(summary model.Summary) bool {
    for _, changed := range summary.ChangedFiles {
        path := strings.ToLower(changed.Path)
        switch filepath.Ext(path) {
        case ".ts", ".tsx", ".mts", ".cts":
            return true
        }
    }

    return false
}
```

Repo conventions:

- Review policy toggles are unit-tested in `internal/review/review_test.go`.
- Path classification is currently extension/path based and intentionally simple.
- Do not add a dependency just to classify code paths.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Review tests | `go test ./internal/review` | exit 0 |
| Model/capture tests | `go test ./internal/model ./internal/capture/fswatcher` | exit 0 if touched |
| Full tests | `go test ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Final gate | `make verify` | exit 0; note that it writes `coverage.out` and `./agentreceipt` |

## Scope

**In scope**:

- `internal/review/review.go`
- `internal/review/review_test.go`
- `internal/model/model.go` only if a new summary helper or field is truly needed
- `internal/capture/fswatcher/fswatcher.go` only if classification must be extended there

**Out of scope**:

- Building a full language detector.
- Requiring tests for every dependency or config change.
- Changing lint prompts unless tests prove they need the same code-change guard.
- Changing typecheck prompts; they already use `hasTypeScriptChanges`.

## Git workflow

- Branch: `advisor/021-code-change-test-prompts`
- Commit message example from repo style: `fix: only prompt for tests on code changes`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add policy tests for docs-only and code changes

In `internal/review/review_test.go`, add tests around `focus` and `gaps`:

- docs-only summary: `ChangedFiles: []model.ChangedFile{{Path: "README.md"}}`, no test command detected. Expect no `"tests were run"` focus item and no `"No test command"` gap.
- Go code summary: `ChangedFiles: []model.ChangedFile{{Path: "internal/review/review.go"}}`, no test command detected. Expect both the focus item and gap.
- TypeScript behavior should remain unchanged for `.ts`/`.tsx`.

**Verify**: `go test ./internal/review -run 'Policy|Test'` -> docs-only test should fail before implementation.

### Step 2: Add a small code-change predicate

In `internal/review/review.go`, add a helper near `hasTypeScriptChanges`, for example `hasCodeChanges(summary model.Summary) bool`. Keep it conservative and explicit.

Recommended initial extensions:

- Go: `.go`
- TypeScript/JavaScript: `.ts`, `.tsx`, `.mts`, `.cts`, `.js`, `.jsx`, `.mjs`, `.cjs`
- Python: `.py`
- Rust: `.rs`
- Java/Kotlin/Scala: `.java`, `.kt`, `.kts`, `.scala`
- C/C++/Obj-C: `.c`, `.h`, `.cc`, `.cpp`, `.cxx`, `.hpp`, `.m`, `.mm`
- Shell: `.sh`, `.bash`, `.zsh`
- Ruby/PHP/Swift: `.rb`, `.php`, `.swift`
- Config that commonly executes or controls builds may be included only if tests document the choice.

Do not classify `.md` as code. Treat files with no extension as non-code unless there is a clear project convention in tests.

**Verify**: `go test ./internal/review -run 'Policy|Test'` -> exit 0.

### Step 3: Gate test prompts with the predicate

Change both focus and gaps:

```go
if cfg.Review.RequireTestsForCodeChanges && hasCodeChanges(summary) && !summary.TestDetected {
    ...
}
```

Leave typecheck logic as-is. Decide whether the lint gap should remain unconditional. This plan's intended change is only test prompts; if lint noise also needs a guard, stop and report a follow-up rather than broadening scope silently.

**Verify**: `go test ./internal/review` -> exit 0.

### Step 4: Full validation

**Verify**:

- `go test ./...` -> exit 0.
- `go vet ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Docs-only changed file does not produce missing-test focus or gap.
- Go code changed file produces missing-test focus and gap.
- TypeScript changed file still produces typecheck prompt when no typecheck command was detected.
- Policy toggle disabling `RequireTestsForCodeChanges` still suppresses test prompts.

## Done criteria

- [ ] Missing-test prompts appear only when at least one changed path is classified as code.
- [ ] README/docs-only changes do not trigger missing-test prompts.
- [ ] Existing typecheck behavior remains unchanged.
- [ ] Existing review policy toggle tests pass.
- [ ] `go test ./...` exits 0.
- [ ] `go vet ./...` exits 0.
- [ ] `make verify` exits 0, or the operator records why the mutating gate was skipped.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- The team wants tests required for dependency/config changes even when no source code file changed.
- Classifying code paths requires repo-specific configuration rather than a small default extension list.
- Existing tests explicitly require docs-only sessions to show missing-test prompts.

## Maintenance notes

Keep this heuristic conservative. False negatives are possible for unusual languages, but noisy false positives in reviewer output have a direct product cost. Future work can add configurable code-path patterns if users need stricter policy.
