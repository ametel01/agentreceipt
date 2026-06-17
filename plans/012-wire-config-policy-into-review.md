# Plan 012: Wire config policy into review decisions

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat c5ab6b4..HEAD -- internal/config internal/review cmd/root.go cmd/root_test.go README.md docs/TECH_SPEC.md`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: L
- **Risk**: MED
- **Depends on**: `plans/010-enforce-provider-privacy-contract.md`
- **Category**: dx
- **Planned at**: commit `c5ab6b4`, 2026-06-17

## Why this matters

AgentReceipt exposes config fields for test commands, review requirements, and path-risk behavior, but review currently uses hard-coded command regexes and does not receive loaded config. Users can believe they configured policy when the final review is not honoring it. This plan makes config-backed review behavior explicit and testable.

## Current state

Relevant files:

- `internal/config/config.go` - defines review policy and test command config.
- `cmd/root.go` - loads config for session manager, but `reviewOptionsFromCommand` returns only repo/session/display flags.
- `internal/review/review.go` - hard-codes command detection and gap logic.
- `README.md` and `docs/TECH_SPEC.md` - describe optional explicit config.

Current excerpts:

```go
// internal/config/config.go:50
type Review struct {
    RequireTestsForCodeChanges bool `yaml:"require_tests_for_code_changes" json:"require_tests_for_code_changes"`
    RequireTypecheckForTS      bool `yaml:"require_typecheck_for_ts" json:"require_typecheck_for_ts"`
    FlagDependencyChanges      bool `yaml:"flag_dependency_changes" json:"flag_dependency_changes"`
    FlagAuthChanges            bool `yaml:"flag_auth_changes" json:"flag_auth_changes"`
    FlagSecretPaths            bool `yaml:"flag_secret_paths" json:"flag_secret_paths"`
}
```

```go
// cmd/root.go:983
func managerFromCommand(cmd *cobra.Command) (session.Manager, error) {
    // loads config into session.Manager
}
```

```go
// internal/review/review.go:89
var commandKindPatterns = []struct {
    kind    string
    pattern *regexp.Regexp
}{ ... }
```

```go
// internal/review/review.go:1043
func reviewOptionsFromCommand(cmd *cobra.Command) (review.Options, error) {
    return review.Options{RepoPath: repoPath, SessionID: sessionID, Last: last, Security: security, Diff: diff}, nil
}
```

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Config tests | `go test ./internal/config` | exit 0 |
| Review tests | `go test ./internal/review` | exit 0 |
| CLI tests | `go test ./cmd` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Final gate | `make verify` | exit 0 |

## Scope

**In scope**:

- `internal/review/review.go`
- `internal/review/review_test.go`
- `internal/config/config.go`
- `internal/config/config_test.go`
- `cmd/root.go`
- `cmd/root_test.go`
- `README.md`
- `docs/TECH_SPEC.md`

**Out of scope**:

- Hosted team policy or GitHub branch protection.
- New policy file format.
- Claude hook implementation.
- Any agent scoring or model scoring.

## Git workflow

- Branch: `advisor/012-config-policy-review`
- Commit message example: `feat: apply config policy in review`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add config to review options

Add `Config config.Config` or a smaller policy struct to `review.Options`. Make `review.Build` use `config.Default()` when no config is supplied so existing callers keep default behavior.

Update `cmd/root.go` so `review`, `export`, or related paths that build review output use the loaded config from `--config`.

**Verify**: `go test ./internal/review ./cmd` -> exit 0.

### Step 2: Replace hard-coded command detection with configured test commands

Use `cfg.TestCommands` for test/lint/typecheck detection where possible. Keep a small built-in classification fallback for command kinds not represented in config, such as network/destructive signals, unless Plan 011 has already centralized provider risk handling.

Required behavior:

- A custom configured test command marks `Summary.TestDetected`.
- Disabling or omitting a command from config should not accidentally mark it as detected.
- Existing default commands behave the same as before.

**Verify**: `go test ./internal/review -run 'Command|Review'` -> exit 0.

### Step 3: Honor review policy toggles

Apply these config fields in `risk`, `focus`, and `gaps`:

- `RequireTestsForCodeChanges`
- `RequireTypecheckForTS`
- `FlagDependencyChanges`
- `FlagAuthChanges`
- `FlagSecretPaths`

If a field cannot be meaningfully implemented yet, remove it from config and docs in the same change rather than leaving it inert. Prefer implementation for the fields that have existing data: dependency and sensitive path flags already have filesystem classification.

**Verify**: `go test ./internal/review` -> exit 0.

### Step 4: Add CLI coverage for `--config` affecting review

In `cmd/root_test.go`, add a test that writes a config with a custom test command, imports a Codex trace containing that command, and confirms `review --json --config <path>` reports `test_detected: true`.

**Verify**: `go test ./cmd` -> exit 0.

### Step 5: Update docs

Update README or TECH_SPEC so documented config fields match implemented behavior exactly. Do not add a broad policy guide; keep this scoped.

**Verify**: `git diff --check -- README.md docs/TECH_SPEC.md cmd/root.go internal/review/review.go` -> no output, exit 0.

### Step 6: Full validation

**Verify**:

- `go test ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Unit tests for default command detection parity.
- Unit tests for custom test command detection.
- Unit tests for each review policy toggle that remains in config.
- CLI test proving `--config` changes review behavior.

## Done criteria

- [ ] Review receives and uses loaded config.
- [ ] No review/config field remains inert without documentation saying it is reserved.
- [ ] Custom `test_commands` affect `test_detected`.
- [ ] Policy toggles affect risk/focus/gaps or are removed from config/docs.
- [ ] `go test ./...` exits 0.
- [ ] `make verify` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Config semantics conflict with docs after Plan 010 lands.
- Implementing policy toggles requires a new repository policy file format.
- A field cannot be implemented without changing public receipt JSON shape.

## Maintenance notes

New config fields must ship with behavior tests. Avoid adding roadmap fields to active config structs unless commands explicitly reject or label them as reserved.
