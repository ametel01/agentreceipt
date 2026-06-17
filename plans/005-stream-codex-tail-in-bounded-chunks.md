# Plan 005: Stream Codex tail parsing in bounded chunks

> **Executor instructions**: Follow this plan step by step. Run every verification command. If a STOP condition occurs, stop and report. Update `plans/README.md` when done.
>
> **Drift check (run first)**: `git diff --stat f9e7997..HEAD -- internal/provider/codex/watch.go internal/provider/codex/codex.go internal/provider/codex/codex_test.go cmd/root.go cmd/root_test.go`
> Compare excerpts below against live code if any in-scope file changed.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none
- **Category**: perf
- **Planned at**: commit `f9e7997`, 2026-06-17

## Why this matters

`start --watch` should stay responsive even when Codex writes a large log burst. `TailFile` currently reads all bytes from the last offset to EOF into memory on every poll. Normal logs may be small, but provider logs are untrusted local input and command outputs can be large; bounded tailing prevents memory spikes and preserves terminal responsiveness.

## Current state

Current excerpts:

```go
// internal/provider/codex/watch.go:97
chunk, err := io.ReadAll(file)
if err != nil {
    return TailResult{}, err
}
...
complete := chunk[:lastNewline+1]
tail.ParseResult = ParseJSONL(bytes.NewReader(complete), ParseOptions{...})
```

```go
// cmd/root.go:367
tail, err := codex.TailFile(candidate.Path, codex.TailOptions{
    SessionID:  state.SessionID,
    CWD:        state.RepoRoot,
    Offset:     tracked.offset,
    LineOffset: tracked.lineOffset,
})
```

Repo conventions:
- Parser uses `bufio.Scanner` with a 1 MiB token limit.
- Watch logic tracks both byte offset and logical line offset.
- Existing tests cover partial trailing lines.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Parser tests | `go test ./internal/provider/codex` | exit 0 |
| Watch command tests | `go test ./cmd -run Watch` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Final gate | `make verify` | exit 0 |

## Scope

**In scope**:
- `internal/provider/codex/watch.go`
- `internal/provider/codex/codex_test.go`
- `cmd/root.go` only to pass a new max tail setting if needed
- `cmd/root_test.go` only for watch behavior tests

**Out of scope**:
- Rewriting the Codex parser.
- Changing watch output formatting.
- Adding SQLite log support.

## Git workflow

- Branch: `advisor/005-bounded-codex-tail`
- Commit message example: `Bound Codex watch tail reads`.
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add bounded tail options

Extend `TailOptions` with a maximum tail bytes field if the existing `MaxOutputBytes` is not semantically appropriate. Use a sane default, for example 1-4 MiB per poll. Keep output redaction truncation separate from file-read chunk sizing.

**Verify**: `go test ./internal/provider/codex` -> compile should pass after updating tests.

### Step 2: Replace `io.ReadAll` with chunked complete-line parsing

Implement bounded reading from offset:

- Read at most max tail bytes per call.
- Only parse complete lines ending in `\n`.
- Advance `NextOffset` only through complete parsed lines.
- If the chunk ends before a newline, keep offset unchanged for that partial line.
- If more complete data remains, allow the next poll to continue from `NextOffset`.

Avoid losing lines that are larger than the chunk. If a single complete JSONL line exceeds the max size, emit a parse warning and advance past it only when safe.

**Verify**: `go test ./internal/provider/codex -run TailFile` -> exit 0.

### Step 3: Add large-tail regression tests

Add tests in `internal/provider/codex/codex_test.go` that create a JSONL file with multiple complete lines exceeding the per-call max. Assert:

- first call parses only bounded complete lines;
- second call continues from `NextOffset`;
- line offsets remain correct;
- partial trailing line remains unparsed until completed.

**Verify**: `go test ./internal/provider/codex -run 'TailFile|Large'` -> exit 0.

### Step 4: Validate watch command behavior

If `cmd/root.go` needs to pass a max tail setting, update watch tests so existing behavior remains unchanged for normal logs.

**Verify**:
- `go test ./cmd -run Watch` -> exit 0.
- `go test ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Existing `TestSessionCWDAndTailFile` must still pass.
- New tests for large complete lines and multi-call bounded progress.
- Watch CLI tests remain unchanged for normal logs.

## Done criteria

- [ ] `TailFile` does not call `io.ReadAll` on the entire remaining file.
- [ ] Large appended logs are processed incrementally without losing complete lines.
- [ ] Partial trailing lines remain safe.
- [ ] `go test ./...` and `make verify` pass.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Scanner token limits make it impossible to handle large JSONL lines without redesigning `ParseJSONL`.
- Bounded reads cause duplicate event imports in watch tests.
- The fix requires changing persisted event IDs or event hash semantics.

## Maintenance notes

Future provider parsers should define both output redaction limits and input read limits. They solve different problems and should not be conflated.
