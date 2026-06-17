# Plan 019: Recognize `make verify` as a default verification command

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 28911ab..HEAD -- internal/config/config.go internal/config/config_test.go internal/review/review.go internal/review/review_test.go cmd/root_test.go Makefile README.md`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: tests
- **Planned at**: commit `28911ab`, 2026-06-17

## Why this matters

AgentReceipt's default command detection does not recognize this repository's own canonical gate, `make verify`, as evidence that tests ran. `make verify` runs format check, lint/staticcheck/vet, tests, race tests, security checks, coverage, build, and smoke, but review summaries can still report no test command when that exact command is observed. This creates false review gaps and weakens the signal AgentReceipt is built to provide.

## Current state

Relevant files:

- `internal/config/config.go` - default `TestCommands`.
- `internal/config/config_test.go` - default config validation tests.
- `internal/review/review.go` - classifies commands by configured test commands.
- `internal/review/review_test.go` - tests default and configured command detection.
- `cmd/root_test.go` - CLI-level configured test command test.
- `Makefile` - declares `verify` and its component gates.

Current excerpts:

```go
// internal/config/config.go:106
TestCommands: []string{
    "npm test",
    "npm run test",
    "npm run lint",
    "npm run typecheck",
    "pnpm test",
    "pnpm lint",
    "pnpm typecheck",
    "yarn test",
    "staticcheck ./...",
    "go vet ./...",
    "tsc --noEmit",
    "pyright",
    "cargo test",
    "pytest",
    "go test ./...",
    "make test",
},
```

```go
// internal/review/review.go:1172
func configuredCommandKind(command string, configured []string) string {
    command = normalizedCommand(command)
    for _, candidate := range configured {
        candidate = normalizedCommand(candidate)
        if candidate == "" {
            continue
        }
        if command != candidate && !strings.HasPrefix(command, candidate+" ") {
            continue
        }
        switch {
        case strings.Contains(candidate, "lint") || strings.Contains(candidate, "staticcheck") || strings.Contains(candidate, "go vet"):
            return "lint"
        case strings.Contains(candidate, "typecheck") || strings.Contains(candidate, "tsc") || strings.Contains(candidate, "pyright"):
            return "typecheck"
        default:
            return "test"
        }
    }

    return ""
}
```

```make
# Makefile:56
verify: fmt-check lint test test-race security coverage build smoke
```

Repo conventions:

- Default config should be useful without a repo-local config file.
- Custom `test_commands` remain supported and are tested in `cmd/root_test.go`.
- The review model currently has booleans for `TestDetected`, `LintDetected`, and `TypecheckDetected`; do not expand the model in this small plan unless required.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Config tests | `go test ./internal/config` | exit 0 |
| Review tests | `go test ./internal/review` | exit 0 |
| CLI tests | `go test ./cmd -run 'Configured|Review'` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Final gate | `make verify` | exit 0; note that it writes `coverage.out` and `./agentreceipt` |

## Scope

**In scope**:

- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/review/review.go`
- `internal/review/review_test.go`
- `cmd/root_test.go` only if CLI coverage needs a default `make verify` fixture

**Out of scope**:

- Adding a generalized Makefile parser.
- Adding new summary booleans for security, race, coverage, build, or smoke.
- Changing user-provided `test_commands` semantics.
- Reclassifying every `make <target>` command as a test command.

## Git workflow

- Branch: `advisor/019-make-verify-detection`
- Commit message example from repo style: `fix: detect make verify in review`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add `make verify` to default commands

In `internal/config/config.go`, add `"make verify"` to `Default().TestCommands`. Put it near `"make test"` so the list remains easy to scan.

Because `configuredCommandKind` treats unmatched configured commands as `"test"` by default, this alone should make `make verify` set `TestDetected`.

**Verify**: `go test ./internal/config` -> exit 0.

### Step 2: Add review regression coverage

Update `internal/review/review_test.go` in `TestConfiguredCommandDetection` or a nearby focused test. Include a command attempt for `make verify` with default config and assert:

- the command kind is `"test"`;
- `summary.TestDetected == true`;
- it does not break existing `staticcheck ./...` and `tsc --noEmit` detection.

Do not assert that `make verify` sets `LintDetected` or `TypecheckDetected` unless you intentionally extend the review model in this plan.

**Verify**: `go test ./internal/review -run ConfiguredCommandDetection` -> exit 0.

### Step 3: Add CLI-level coverage if the gap is only caught at integration level

If the unit test is enough, skip this step. If an executor finds CLI import/review has a separate path, add a small `cmd/root_test.go` case that imports a Codex JSONL command with `cmd: "make verify"` and asserts review JSON contains `"test_detected": true`.

**Verify**: `go test ./cmd -run 'Review|Configured'` -> exit 0.

### Step 4: Full validation

**Verify**:

- `go test ./internal/config ./internal/review ./cmd` -> exit 0.
- `go test ./...` -> exit 0.
- `go vet ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Unit test that default config classifies `make verify` as test evidence.
- Existing custom `test_commands` test remains unchanged: custom commands should still be honored.
- Optional CLI test proving imported provider evidence with `make verify` affects review JSON.

## Done criteria

- [ ] `config.Default().TestCommands` includes `make verify`.
- [ ] `make verify` command evidence sets `summary.TestDetected`.
- [ ] Existing custom command detection still passes.
- [ ] `go test ./...` exits 0.
- [ ] `go vet ./...` exits 0.
- [ ] `make verify` exits 0, or the operator records why the mutating gate was skipped.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- The team decides `make verify` should be represented as a richer aggregate command rather than `"test"`.
- Adding `make verify` to defaults conflicts with an existing documented policy.
- The fix requires parsing arbitrary Makefile targets.

## Maintenance notes

This plan intentionally treats `make verify` as test evidence because the current model has no aggregate verification kind. A future review-model expansion could distinguish tests, lint, typecheck, security, coverage, and smoke separately, but that should be a separate plan.
