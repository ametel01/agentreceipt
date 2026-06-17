# Plan 001: Run the filesystem watcher during active sessions

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in "STOP conditions" occurs, stop and report. When done, update this plan's status row in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat f9e7997..HEAD -- internal/session/session.go internal/session/session_test.go internal/capture/fswatcher/fswatcher.go internal/capture/fswatcher/fswatcher_test.go`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding. On mismatch, stop and report.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `f9e7997`, 2026-06-17

## Why this matters

AgentReceipt claims high-confidence filesystem evidence, but sessions currently do not run the filesystem watcher. `Start` constructs the watcher only to verify initialization and immediately closes it, while `status` reports filesystem capture as `ready`. This means sensitive path and dependency changes can be absent from the event log, undermining review risk summaries.

## Current state

- `internal/session/session.go` owns `Manager.Start`, `Manager.Stop`, and active session state.
- `internal/capture/fswatcher/fswatcher.go` already implements a watcher with `Start(ctx)`, `Events()`, and debounced `fs.change` events.
- `internal/session/session_test.go` tests lifecycle behavior but expects only git snapshot/finalizer events.

Current excerpts:

```go
// internal/session/session.go:93
fsWatcher, err := BuildFilesystemWatcher(repoRoot, sessionID, m.Config)
if err != nil {
    return State{}, err
}
if err := fsWatcher.Close(); err != nil {
    return State{}, err
}
```

```go
// internal/session/session.go:124
CaptureSources: CaptureSources{
    Git:        "active",
    Filesystem: "ready",
    CodexLogs:  "not_observed",
},
```

```go
// internal/capture/fswatcher/fswatcher.go:106
func (w *Watcher) Start(ctx context.Context) error {
    if err := w.watchTree(); err != nil {
        return err
    }
    go w.run(ctx)

    return nil
}
```

Repo conventions:
- Use explicit errors and typed state, no silent fallbacks in core paths.
- Tests use temporary git repos and helper functions in the package under test.
- Storage artifacts live under global AgentReceipt storage, not repo-local files.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Focused tests | `go test ./internal/session ./internal/capture/fswatcher` | exit 0 |
| Review tests | `go test ./internal/review ./internal/receipt ./cmd` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Final gate | `make verify` | exit 0; may refresh ignored/generated local artifacts |

## Scope

**In scope**:
- `internal/session/session.go`
- `internal/session/session_test.go`
- `internal/capture/fswatcher/fswatcher.go` only if a small API adjustment is required
- `internal/capture/fswatcher/fswatcher_test.go` only for regression coverage

**Out of scope**:
- Rewriting the watcher implementation.
- Changing receipt/review risk policy beyond consuming real `fs.change` events.
- Adding repo-local `.agentreceipt` files.

## Git workflow

- Branch: `advisor/001-run-filesystem-watcher`
- Commit message style: use the repo's existing conventional-ish style, for example `Fix filesystem watcher session capture`.
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add session-owned filesystem watcher lifetime

Update `Manager.Start` so a watcher remains running while the session is active. Because `agentreceipt start` is a short-lived command today, do not just start a goroutine inside that process and return; that would die immediately. Choose one of these explicit approaches:

- Preferred: make the session manager own a background sidecar process or durable watcher lifecycle that survives the `start` command, matching the technical spec's sidecar process intent.
- Minimal acceptable MVP: if durable process management is not present yet, change state text from `ready` to a truthful value such as `initialized_only` and implement the durable watcher in a follow-up. If you choose this fallback, mark the plan BLOCKED in `plans/README.md` and explain why production process lifetime is missing.

If implementing the durable path, append `fs.change` events to `events.jsonl` using the existing `eventlog.Writer` pattern and keep the event chain valid.

**Verify**: `go test ./internal/session` -> expected to compile; failing tests should be from newly added assertions only, not unrelated packages.

### Step 2: Add lifecycle regression coverage

Add a test in `internal/session/session_test.go` that starts a session, modifies a file, waits for an `fs.change` event to appear in `events.jsonl`, and then stops the session. The test must assert:

- at least one event has `Source == "fs_watcher"` and `Type == "fs.change"`;
- the changed path is relative to the repo;
- sensitive/dependency classification is preserved for at least one fixture path, such as `go.mod` or `.env`;
- `eventlog.Replay` still succeeds.

Use existing helper style from `TestStartStatusLiveStopLifecycle`.

**Verify**: `go test ./internal/session -run 'Test.*Filesystem|TestStartStatusLiveStopLifecycle'` -> exit 0.

### Step 3: Ensure review consumes real watcher evidence

Run a small integration path through review/receipt after the watcher emits events. If existing review behavior already works once real `fs.change` events exist, do not modify review code. If review misses the events, adjust only the summary path in `internal/review/review.go`.

**Verify**: `go test ./internal/review ./internal/receipt` -> exit 0.

### Step 4: Full validation

Run the broader checks.

**Verify**:
- `go test ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Add one session lifecycle test proving a real filesystem write becomes an event.
- Add or adjust one review/receipt integration test only if real watcher evidence is not already summarized correctly.
- Reuse existing temp git repo helpers and `eventlog.Replay` assertions.

## Done criteria

- [ ] Production session capture emits `fs_watcher` events for file changes while active.
- [ ] `status` no longer overstates filesystem capture if durable watching is not available.
- [ ] Event chain replay remains valid after filesystem events.
- [ ] `go test ./...` exits 0.
- [ ] `make verify` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- A durable watcher requires adding a daemon/process supervisor that is larger than this plan.
- The live code no longer matches the current-state excerpts.
- Fixing this requires changing storage layout or receipt schema.
- Tests become flaky because filesystem notifications are not deterministic on one supported OS.

## Maintenance notes

Reviewers should scrutinize process lifetime and cleanup. The key question is whether filesystem capture truly continues after `agentreceipt start` returns and before `agentreceipt stop` runs.
