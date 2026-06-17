# Plan 002: Preserve command result status in review summaries

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving on. If a STOP condition occurs, stop and report. Update `plans/README.md` when done.
>
> **Drift check (run first)**: `git diff --stat f9e7997..HEAD -- internal/provider/codex/codex.go internal/review/review.go internal/review/review_test.go cmd/root_test.go`
> If any in-scope file changed since this plan was written, compare the excerpts below against live code before proceeding.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `f9e7997`, 2026-06-17

## Why this matters

The watch view can show `ok` or `fail`, but `review` currently reports detected commands with `Status: "unknown"` because it ignores `provider.command_result` events. PR reviewers need to know whether test/lint/typecheck commands succeeded, failed, or were only observed as attempted. Without status propagation, receipts lose one of their highest-value review signals.

## Current state

- `internal/provider/codex/codex.go` emits both command attempts and command results.
- `internal/review/review.go` summarizes only `provider.command` attempts.
- Tests assert command kind detection, not pass/fail propagation.

Current excerpts:

```go
// internal/provider/codex/codex.go:283
r.Commands = append(r.Commands, CommandEvent{
    SessionID:  options.SessionID,
    LineNumber: r.LineCount,
    CallID:     callID,
    Command:    redact(command, options.MaxOutputBytes),
    Status:     "unknown",
})
```

```go
// internal/provider/codex/codex.go:344
r.Events = append(r.Events, model.Event{
    Type:     "provider.command_result",
    Provider: "codex",
    Payload: map[string]any{
        "line_no":        r.LineCount,
        "command_result": commandEvent,
    },
})
```

```go
// internal/review/review.go:766
if event.Type == "provider.command" {
    command := commandFromPayload(event.Payload)
    if command != "" {
        commands = append(commands, model.DetectedCommand{
            Command: command,
            Status:  "unknown",
        })
    }
}
```

Repo conventions:
- Review output is built from typed `model.Event` values and rendered deterministically.
- Existing tests use synthetic provider events to keep behavior focused.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Focused review tests | `go test ./internal/review` | exit 0 |
| Parser tests | `go test ./internal/provider/codex ./cmd` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Final gate | `make verify` | exit 0 |

## Scope

**In scope**:
- `internal/review/review.go`
- `internal/review/review_test.go`
- `internal/provider/codex/codex.go` only if command-result payloads need small normalization
- `cmd/root_test.go` only if CLI output assertions need coverage

**Out of scope**:
- Changing the Codex JSONL parser's public schemas beyond status fields.
- Redesigning risk classification.
- Changing watch renderer output.

## Git workflow

- Branch: `advisor/002-command-result-status`
- Commit message example: `Fix review command result status`.
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Correlate attempts and results

Update `summarize` in `internal/review/review.go` so it collects command attempts from `provider.command` and results from `provider.command_result`, preferably keyed by `call_id` when present. Preserve command text from the attempt because result payloads may not contain the command.

Target behavior:

- attempt with no result -> `Status: "unknown"`;
- result status `success` -> detected command status `success`;
- result status `failed` -> detected command status `failed`;
- exit code should be retained if a model field exists; if no field exists, add only if needed and update JSON tests.

**Verify**: `go test ./internal/review` -> initially update tests until it passes.

### Step 2: Make tests cover success and failure

Add a regression test to `internal/review/review_test.go` that appends a `provider.command` event and a matching `provider.command_result` event. Assert:

- command is detected once;
- command kind remains `test`, `lint`, or relevant kind;
- status is `success` for exit 0;
- status is `failed` for a non-zero result in a second case.

Use existing `TestBuildReviewFromSessionEvents` and `TestBuildReviewFromActiveSessionRiskAndConfidenceSignals` as patterns.

**Verify**: `go test ./internal/review -run Command` -> exit 0.

### Step 3: Ensure rendered review exposes useful status if needed

If terminal or Markdown review output lists commands in a place where status should be visible, add a compact command evidence section. Keep it concise and deterministic. If current output intentionally shows only counts, do not expand output in this plan; just ensure JSON/receipt summaries carry status.

**Verify**: `go test ./cmd ./internal/review` -> exit 0.

### Step 4: Full validation

**Verify**:
- `go test ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Add review tests for attempted command with success result.
- Add review tests for attempted command with failure result.
- Ensure existing parser/watch tests still pass.

## Done criteria

- [ ] `review --json` summaries include command statuses other than `unknown` when Codex result evidence exists.
- [ ] Attempt-only commands still produce `unknown`.
- [ ] New tests prove success and failure propagation.
- [ ] `go test ./...` exits 0.
- [ ] `make verify` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Provider result events cannot be reliably correlated with command attempts.
- The fix requires a receipt schema migration beyond adding optional fields.
- Existing tests reveal multiple incompatible command event shapes not covered by this plan.

## Maintenance notes

Future provider integrations should emit a stable command attempt/result pair so review summaries do not need provider-specific correlation logic.
