# Plan 014: Implement Codex install detection

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat c5ab6b4..HEAD -- cmd/root.go cmd/root_test.go internal/provider/codex README.md docs/TECH_SPEC.md`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none
- **Category**: direction
- **Planned at**: commit `c5ab6b4`, 2026-06-17

## Why this matters

`agentreceipt install codex` is part of the command surface, but it is still a scaffold. The rest of the CLI already knows how to inspect Codex homes and parse logs, so this is a cheap product-completion slice: make the install command report concrete local Codex evidence availability instead of a planned behavior message.

## Current state

Relevant files:

- `cmd/root.go` - `install codex` is currently created by `newScaffoldCommand`.
- `internal/provider/codex/codex.go` - `Inspect` finds session candidates under `sessions` and `archived_sessions`.
- `cmd/root_test.go` - command surface and scaffold tests.
- `README.md` - current workflow examples.

Current excerpts:

```go
// cmd/root.go:151
newScaffoldCommand("codex", "Detect local Codex logs and configure parser defaults", "install codex will detect Codex log directories and update local parser preferences."),
```

```go
// internal/provider/codex/codex.go:431
func Inspect(home string) InspectResult {
    if home == "" {
        home = os.Getenv("CODEX_HOME")
    }
    ...
}
```

Product constraints:

- README says AgentReceipt does not write repo-local `.agentreceipt` or policy files.
- AgentReceipt should run beside Codex, not wrap or control it.
- Codex logs are best-effort evidence, not required for receipt finalization.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| CLI tests | `go test ./cmd` | exit 0 |
| Codex parser tests | `go test ./internal/provider/codex` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Smoke | `./scripts/smoke.sh` | exit 0 |
| Final gate | `make verify` | exit 0 |

## Scope

**In scope**:

- `cmd/root.go`
- `cmd/root_test.go`
- `internal/provider/codex/codex.go`
- `internal/provider/codex/codex_test.go`
- `README.md`
- `docs/TECH_SPEC.md`

**Out of scope**:

- Writing Codex config files.
- Installing or modifying Codex itself.
- Adding a wrapper or launcher.
- Persisting repo-local AgentReceipt config.

## Git workflow

- Branch: `advisor/014-install-codex-detection`
- Commit message example: `feat: implement codex install detection`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Replace the scaffold with a real command

Create `newInstallCodexCommand()` and wire it into `newInstallCommand`. The command should:

- accept `--home` with the same semantics as `inspect codex --home`;
- call `codex.Inspect`;
- print the resolved Codex home, candidate count, warning count, and a concise next command such as `agentreceipt start --watch`;
- exit 0 when no logs are found, because missing provider evidence is non-fatal.

**Verify**: `go test ./cmd -run 'Install|CommandTree'` -> exit 0.

### Step 2: Keep behavior read-only

Do not write config by default. If the command detects no logs, print an explicit warning using existing `codex.Inspect` warnings. If it detects logs, print the newest candidate path and whether `CODEX_HOME` or default `~/.codex` was used.

**Verify**: add tests for missing-home and candidate-home output, then run `go test ./cmd` -> exit 0.

### Step 3: Update docs and smoke

Update README only if command examples or wording need to mention detection. Update `scripts/smoke.sh` if it currently expects the scaffolded text.

**Verify**:

- `./scripts/smoke.sh` -> exit 0.
- `git diff --check -- README.md scripts/smoke.sh cmd/root.go` -> no output, exit 0.

### Step 4: Full validation

**Verify**:

- `go test ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- CLI test for `install codex --home <missing>` showing warnings.
- CLI test for `install codex --home <temp-codex-home>` showing candidates.
- Existing scaffold test should be updated so only intentionally deferred commands remain scaffolded.

## Done criteria

- [ ] `agentreceipt install codex` performs useful read-only detection.
- [ ] No repo-local files are written.
- [ ] Missing logs are explicit and non-fatal.
- [ ] README/smoke expectations match the new behavior.
- [ ] `go test ./...` exits 0.
- [ ] `make verify` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Product owner wants `install codex` to write persistent parser preferences in this slice.
- Detection requires relying on undocumented Codex files beyond those already used by `inspect codex`.
- README currently promises behavior that conflicts with read-only detection.

## Maintenance notes

Keep `install codex` as a detection/setup helper, not a wrapper. If future persistent preferences are added, they should follow Plan 012's config semantics.
