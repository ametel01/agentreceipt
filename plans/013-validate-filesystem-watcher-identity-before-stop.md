# Plan 013: Validate filesystem watcher identity before signaling

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat c5ab6b4..HEAD -- internal/session/session.go internal/session/session_test.go cmd/root.go cmd/root_test.go`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S-M
- **Risk**: MED
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `c5ab6b4`, 2026-06-17

## Why this matters

`agentreceipt stop` writes a stop marker and waits for the filesystem watcher to acknowledge it. If the acknowledgement never appears, it signals and then kills the recorded PID. PIDs can be stale or reused, so the fallback should confirm the process is still the AgentReceipt watcher before sending a kill signal.

## Current state

Relevant files:

- `internal/session/session.go` - starts and stops the filesystem watcher sidecar.
- `internal/session/session_test.go` - watcher lifecycle tests.
- `cmd/root.go` - hidden `__internal-fswatcher` command.

Current excerpts:

```go
// internal/session/session.go:758
command := exec.CommandContext(ctx, executable,
    "--repo", state.RepoRoot,
    "__internal-fswatcher",
    "--session", state.SessionID,
    "--config-json", string(configJSON),
)
```

```go
// internal/session/session.go:810
process, err := os.FindProcess(state.FilesystemWatcherPID)
_ = process.Signal(os.Interrupt)
time.Sleep(100 * time.Millisecond)
_ = process.Kill()
```

```go
// cmd/root.go:931
func newInternalFilesystemWatcherCommand() *cobra.Command {
    internalCmd := &cobra.Command{
        Use:    "__internal-fswatcher",
        Hidden: true,
```

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Session tests | `go test ./internal/session` | exit 0 |
| CLI tests | `go test ./cmd` | exit 0 |
| Race check | `go test -race ./internal/session` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Final gate | `make verify` | exit 0 |

## Scope

**In scope**:

- `internal/session/session.go`
- `internal/session/session_test.go`
- `cmd/root.go`
- `cmd/root_test.go`

**Out of scope**:

- Replacing the watcher sidecar architecture.
- Changing event-log append behavior; that is Plan 009.
- Adding daemon supervision.

## Git workflow

- Branch: `advisor/013-watcher-identity-stop`
- Commit message example: `fix: validate filesystem watcher before stop fallback`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Persist watcher identity metadata

When starting the watcher, write enough metadata to prove identity during stop fallback. Options include:

- PID plus expected executable path and session ID.
- A watcher nonce passed to `__internal-fswatcher` and written to a sidecar metadata file.

Prefer a nonce because it avoids relying on platform-specific process command-line inspection. If you add a nonce, pass it as a hidden flag to `__internal-fswatcher`, validate it in `RunFilesystemWatcher`, and write it beside `fswatcher.pid`.

**Verify**: `go test ./internal/session ./cmd` -> exit 0.

### Step 2: Validate before interrupt/kill fallback

Update `stopFilesystemWatcher` so the normal stop-marker path remains unchanged, but the fallback checks identity before sending `os.Interrupt` or `Kill`. If identity cannot be verified, return an explicit error instead of signaling the PID.

**Verify**: `go test ./internal/session` -> exit 0.

### Step 3: Add stale PID regression tests

Add tests that cover:

- normal done-marker stop still succeeds;
- stale or mismatched watcher identity returns an error before kill fallback;
- missing identity metadata produces a clear error only when fallback is needed.

Use test seams rather than killing real unrelated processes.

**Verify**: `go test -race ./internal/session` -> exit 0.

### Step 4: Full validation

**Verify**:

- `go test ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Add unit tests around `stopFilesystemWatcher`.
- Keep existing production-launch and marker tests passing.
- Avoid tests that depend on actual OS PID reuse.

## Done criteria

- [ ] Stop fallback validates watcher identity before signaling a PID.
- [ ] Stale or mismatched PID cases fail safely with a clear error.
- [ ] Normal watcher stop remains compatible.
- [ ] `go test ./...` exits 0.
- [ ] `make verify` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- The only feasible implementation requires platform-specific process inspection.
- Adding identity metadata breaks existing session layout or manifest verification.
- Plan 009 changes watcher append/stop coordination in a way that supersedes this fallback.

## Maintenance notes

Future sidecar processes should carry an identity token or equivalent ownership proof. Avoid using a bare PID as authority to signal or kill.
