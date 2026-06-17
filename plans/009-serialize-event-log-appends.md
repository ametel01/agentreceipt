# Plan 009: Serialize event-log appends across processes

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat c5ab6b4..HEAD -- internal/eventlog internal/session internal/eventlog/eventlog_test.go internal/session/session_test.go`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `c5ab6b4`, 2026-06-17

## Why this matters

AgentReceipt's receipt integrity depends on `events.jsonl` being a single valid hash chain. Today multiple processes can append to the same session log: the foreground watch/import path and the filesystem watcher sidecar both read the current log, replay the hash, and open an append writer. If they race, both can choose the same next sequence and previous hash, producing a log that fails replay even though each individual append succeeded.

## Current state

Relevant files:

- `internal/eventlog/eventlog.go` - hash-chain writer and replay verification.
- `internal/session/session.go` - session operations that append provider, manual marker, and filesystem watcher events.
- `internal/eventlog/eventlog_test.go` - event-log unit tests.
- `internal/session/session_test.go` - session lifecycle and watcher tests.

Current excerpts:

```go
// internal/session/session.go:311
events, err := eventlog.ReadFile(layout.EventsJSONL)
prevHash, err := eventlog.Replay(events)
writer, err := eventlog.NewWriter(layout.EventsJSONL, prevHash, int64(len(events)+1))
```

```go
// internal/session/session.go:824
events, err := eventlog.ReadFile(layout.EventsJSONL)
prevHash, err := eventlog.Replay(events)
writer, err := eventlog.NewWriter(layout.EventsJSONL, prevHash, int64(len(events)+1))
```

```go
// internal/eventlog/eventlog.go:128
file, err := root.OpenFile(filepath.Base(path), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
```

Repo conventions:

- Tests use package-local helpers and temp directories.
- The canonical gate is `make verify`; focused development should use `go test ./internal/eventlog ./internal/session` first.
- Keep the hash-chain model deterministic; do not relax `eventlog.Replay`.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Event log tests | `go test ./internal/eventlog` | exit 0 |
| Session tests | `go test ./internal/session` | exit 0 |
| Race check | `go test -race ./internal/eventlog ./internal/session` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Final gate | `make verify` | exit 0 |

## Scope

**In scope**:

- `internal/eventlog/eventlog.go`
- `internal/eventlog/eventlog_test.go`
- `internal/session/session.go`
- `internal/session/session_test.go`

**Out of scope**:

- Changing event schema fields.
- Changing receipt signature semantics.
- Replacing JSONL storage with a database.
- Any provider-specific parser changes.

## Git workflow

- Branch: `advisor/009-event-log-append-lock`
- Commit message example: `fix: serialize event log appends`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add a cross-process append lock API

Add an event-log helper that serializes the whole read/replay/append/update sequence for one `events.jsonl` file. A small API shape is acceptable, for example:

```go
// internal/eventlog/eventlog.go
func WithAppendLock(path string, fn func() error) error
```

or a lock-returning helper if that fits the code better. The lock must work across processes, not just goroutines. Prefer an adjacent lock file under the same session directory, opened with `os.OpenRoot` and guarded with the standard library on supported platforms. If Go 1.26 exposes a better portable file-lock primitive, use that; otherwise use a narrow platform-compatible implementation and tests.

**Verify**: `go test ./internal/eventlog` -> exit 0.

### Step 2: Wrap every session event-log append sequence

In `internal/session/session.go`, wrap these operations with the new lock:

- `AppendProviderEvents`
- `Mark`
- `appendFilesystemEvent`

Inside the lock, reread `events.jsonl`, replay it, create the writer, append the new events, then update state/manifest from the appended result. Preserve the existing warning and capture-source behavior.

**Verify**: `go test ./internal/session` -> exit 0.

### Step 3: Add a regression test for competing appenders

Add a test that starts from one event log and performs concurrent appends through the same public/helper path. The test should fail against the current race-prone shape by producing duplicate sequence numbers or replay failure, and pass after locking. It can live in `internal/eventlog/eventlog_test.go` if the new lock API is exposed there, or in `internal/session/session_test.go` if easier to exercise through `AppendProviderEvents` plus filesystem-event append helpers.

Required assertions:

- `eventlog.ReadFile` returns all expected appended events.
- `eventlog.Replay` exits with no error.
- Event `Seq` values are contiguous.

**Verify**: `go test -race ./internal/eventlog ./internal/session` -> exit 0.

### Step 4: Full validation

Run the full suite.

**Verify**:

- `go test ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Add one regression test for concurrent appends to the same session event log.
- Keep existing replay tests intact; do not weaken sequence or hash validation.
- Run `go test -race ./internal/eventlog ./internal/session` to catch goroutine regressions even though the original issue is interprocess.

## Done criteria

- [ ] All appenders that can run during an active session serialize the read/replay/write sequence.
- [ ] A regression test proves competing appenders still produce a valid replayable chain.
- [ ] No event schema or receipt signature fields change.
- [ ] `go test ./...` exits 0.
- [ ] `make verify` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Implementing cross-process locking requires a non-standard dependency.
- The lock cannot be made portable across Linux and macOS.
- The fix appears to require changing `model.Event` or receipt verification fields.
- The drift check shows session append code has already been redesigned.

## Maintenance notes

Future code that appends to `events.jsonl` must use the same locked path. Reviewers should reject new read/replay/`NewWriter` sequences outside the append-lock helper.
