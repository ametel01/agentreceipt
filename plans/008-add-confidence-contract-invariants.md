# Plan 008: Add confidence contract invariants

> **Executor instructions**: Follow this plan after Plan 001. Run every verification command and update `plans/README.md` when done. Stop and report if any STOP condition occurs.
>
> **Drift check (run first)**: `git diff --stat f9e7997..HEAD -- internal/session internal/review internal/receipt internal/capture docs/TECH_SPEC.md README.md`
> If in-scope paths changed, compare current-state excerpts and product wording against live code.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW
- **Depends on**: `plans/001-run-filesystem-watcher-during-sessions.md`
- **Category**: tests
- **Planned at**: commit `f9e7997`, 2026-06-17

## Why this matters

AgentReceipt's product promise depends on evidence confidence labels being true. The audit found a concrete mismatch: filesystem confidence/status could be reported even though the watcher was not running. This plan adds invariant tests so future code cannot claim high-confidence evidence unless corresponding events or artifacts exist.

## Current state

Product docs say:

```md
README.md
AgentReceipt uses three sources in this order:
1. Git monitor (high confidence)
2. Filesystem watcher (high confidence)
3. Codex session logs (best effort)
```

Current code:

```go
// internal/review/review.go:802
func confidence(events []model.Event) model.CaptureConfidence {
    confidence := model.CaptureConfidence{
        GitDiff:            model.ConfidenceNone,
        FilesystemWrites:   model.ConfidenceNone,
        ProviderToolEvents: model.ConfidenceNone,
    }
    for _, event := range events {
        switch event.Source {
        case "git_monitor":
            confidence.GitDiff = model.ConfidenceHigh
        case "fs_watcher":
            confidence.FilesystemWrites = model.ConfidenceHigh
        case "codex_session_log":
            confidence.ProviderToolEvents = model.ConfidenceMedium
        }
    }
}
```

```go
// internal/session/session.go:124
CaptureSources: CaptureSources{
    Git:        "active",
    Filesystem: "ready",
    CodexLogs:  "not_observed",
}
```

Repo conventions:
- Tests are package-local and use synthetic events where appropriate.
- Receipt finalization should warn/downgrade for missing provider evidence, not fail.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Session/review tests | `go test ./internal/session ./internal/review ./internal/receipt` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Coverage | `go test -cover ./...` | exit 0 |
| Final gate | `make verify` | exit 0 |

## Scope

**In scope**:
- `internal/session/session_test.go`
- `internal/review/review_test.go`
- `internal/receipt/receipt_test.go`
- `internal/session/session.go`, `internal/review/review.go`, or `internal/receipt/receipt.go` only if tests reveal mismatched confidence behavior
- `docs/TECH_SPEC.md` or `README.md` only to correct wording if behavior intentionally differs

**Out of scope**:
- New provider integrations.
- Risk policy redesign.
- UI or terminal layout changes.

## Git workflow

- Branch: `advisor/008-confidence-invariants`
- Commit message example: `test: add capture confidence invariants`.
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add session source-state invariant tests

After Plan 001, add tests proving:

- `CaptureSources.Filesystem` reflects the actual watcher lifecycle;
- stopping a session with no provider events downgrades only provider evidence;
- active sessions do not claim imported Codex logs before provider events exist.

**Verify**: `go test ./internal/session` -> exit 0.

### Step 2: Add review confidence invariant tests

In `internal/review/review_test.go`, add table-driven tests for `confidence(events)` using synthetic events:

- no events -> all relevant confidence `none` except documented defaults;
- only git -> git high, filesystem none, provider none;
- git + fs -> git high, filesystem high;
- git + provider -> provider medium;
- malformed/warning-only provider evidence should not overstate tool-event confidence unless a real provider event exists.

**Verify**: `go test ./internal/review -run Confidence` -> exit 0.

### Step 3: Add receipt warning/downgrade coverage

Add or update receipt tests so finalized receipts reflect confidence accurately:

- missing provider events produce warning and provider confidence downgrade;
- filesystem confidence is high only when actual `fs_watcher` events or equivalent artifacts exist.

**Verify**: `go test ./internal/receipt` -> exit 0.

### Step 4: Full validation

**Verify**:
- `go test ./...` -> exit 0.
- `go test -cover ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Add invariant tests for session source state.
- Add table tests for review confidence.
- Add receipt confidence/warning regression coverage.

## Done criteria

- [ ] Tests fail if source confidence is claimed without corresponding evidence.
- [ ] Session status and receipt confidence agree.
- [ ] Missing provider evidence remains non-fatal but visible.
- [ ] `go test ./...` and `make verify` pass.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Product docs and code intentionally disagree on confidence semantics.
- Fixing invariant failures requires completing Plan 001 first and it is not done.
- A test would require sleeps/flaky filesystem timing beyond existing watcher patterns.

## Maintenance notes

Use these invariant tests as guardrails for Claude support and future GitHub policy checks. They are especially important before CI enforcement exists.
