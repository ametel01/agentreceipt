# Plan 018: Record the actual provider in signed receipts

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 28911ab..HEAD -- internal/receipt/receipt.go internal/receipt/receipt_test.go internal/review/review.go internal/review/review_test.go cmd/root_test.go docs/CLAUDE_PROVIDER_DESIGN.md`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `28911ab`, 2026-06-17

## Why this matters

The review layer already identifies Codex, Claude, and mixed provider sessions. The signed receipt still hard-codes `agent.provider` to `codex`. Once Claude hook ingestion exists, a Claude-only session can produce a signed receipt that names the wrong provider, which makes the receipt misleading even when the event evidence is otherwise valid.

## Current state

Relevant files:

- `internal/receipt/receipt.go` - creates `model.Receipt` in `Finalize`.
- `internal/receipt/receipt_test.go` - receipt finalization tests.
- `internal/review/review.go` - already computes provider display labels from events.
- `internal/review/review_test.go` - tests Codex, Claude, and mixed provider labels.
- `cmd/root_test.go` - proves Claude hook ingestion appears in review JSON.
- `docs/CLAUDE_PROVIDER_DESIGN.md` - states provider evidence should be provider-neutral.

Current excerpts:

```go
// internal/receipt/receipt.go:64
receipt := model.Receipt{
    SchemaVersion: model.SchemaVersion,
    SessionID:     sessionID,
    CreatedAt:     generatedAt(options).UTC(),
    Mode:          "sidecar",
    Agent: model.Agent{
        Provider:           "codex",
        ProviderConfidence: report.Confidence.ProviderToolEvents,
    },
    Repo:              model.Repo{Root: repoRoot, DirtyEnd: len(report.Summary.ChangedFiles) > 0},
    Summary:           report.Summary,
    CaptureConfidence: report.Confidence,
    Risk:              report.Risk,
    Verification:      report.Verification,
    Warnings:          report.Warnings,
}
```

```go
// internal/review/review.go:900
func providerLabel(events []model.Event) string {
    providers := map[string]bool{}
    for _, event := range events {
        if !isProviderToolEvidenceEvent(event) {
            continue
        }
        switch {
        case event.Provider != "":
            providers[event.Provider] = true
        case event.Source == "codex_session_log":
            providers["codex"] = true
        case event.Source == "claude_hook":
            providers["claude"] = true
        }
    }
    switch {
    case providers["codex"] && providers["claude"]:
        return "Codex CLI + Claude Code"
    case providers["claude"]:
        return "Claude Code"
    case providers["codex"]:
        return "Codex CLI"
    default:
        return "unknown"
    }
}
```

```go
// cmd/root_test.go:935
stdout, _, err = executeCommand(t, "--repo", repo, "review", "--json")
...
for _, want := range []string{
    `"provider": "Claude Code"`,
    `"command": "go test ./..."`,
    `"status": "success"`,
    `"provider_tool_events": "medium"`,
} {
```

Design constraints:

- `docs/CLAUDE_PROVIDER_DESIGN.md` says Claude evidence should normalize into the existing `model.Event` shape so review logic stays provider-neutral.
- Git and filesystem evidence remain the core proof; provider labels are metadata, not trust scores.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Review tests | `go test ./internal/review` | exit 0 |
| Receipt tests | `go test ./internal/receipt` | exit 0 |
| CLI tests | `go test ./cmd` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Final gate | `make verify` | exit 0; note that it writes `coverage.out` and `./agentreceipt` |

## Scope

**In scope**:

- `internal/receipt/receipt.go`
- `internal/receipt/receipt_test.go`
- `internal/review/review.go` only if provider-label logic must be exported or shared
- `internal/review/review_test.go` only if shared logic tests need adjustment
- `cmd/root_test.go` only if an end-to-end receipt test is clearer there

**Out of scope**:

- Redesigning provider confidence levels.
- Changing Claude hook parsing.
- Adding transcript import.
- Changing receipt schema fields beyond `agent.provider` value selection.

## Git workflow

- Branch: `advisor/018-receipt-provider`
- Commit message example from repo style: `fix: record actual provider in receipts`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add receipt tests for provider labels

In `internal/receipt/receipt_test.go`, add tests that finalize receipts for:

- a Claude-only provider session, expecting `receipt.Agent.Provider == "Claude Code"` or the exact canonical provider string chosen below;
- a mixed Codex + Claude session, expecting the same mixed string review already emits;
- a missing-provider session, expecting `unknown`.

Use existing manager/start/append/stop/finalize helper patterns from the file. The provider events can be minimal `model.Event` values with `Type: "provider.command"`, `Provider: "claude"` or `Provider: "codex"`, and the matching source (`"claude_hook"` or `"codex_session_log"`).

**Verify**: `go test ./internal/receipt -run Provider` -> the new test should fail before implementation because receipts currently say `codex`.

### Step 2: Reuse provider detection for receipts

Avoid duplicating provider-label rules in a divergent way. Preferred approach:

- move the provider-label helper into a small exported function in `internal/review`, for example `ProviderLabel(events []model.Event) string`, and have the existing private `providerLabel` call it or be renamed;
- or add a method/field to `review.Report` that `receipt.Finalize` already receives from `review.Build`.

Since `review.Build` already sets `report.Provider`, the smallest fix is likely in `receipt.Finalize`: set `Agent.Provider: report.Provider` instead of the literal `"codex"`.

**Verify**: `go test ./internal/receipt -run Provider` -> exit 0.

### Step 3: Confirm review behavior does not regress

Run existing review tests and keep labels stable:

- Codex-only: `Codex CLI`.
- Claude-only: `Claude Code`.
- Mixed: `Codex CLI + Claude Code`.
- Warning-only provider records: `unknown`.

**Verify**: `go test ./internal/review -run ProviderLabel` -> exit 0.

### Step 4: Add a CLI-level regression only if receipt tests do not cover finalization

If `internal/receipt` tests already exercise a finalized receipt from session events, skip this step. If not, extend `cmd/root_test.go` near `TestInternalClaudeHookImportsIntoActiveSessionReview` to stop/finalize and export JSON, then assert the receipt provider matches Claude. Do not make this test depend on real Claude installation.

**Verify**: `go test ./cmd -run Claude` -> exit 0.

### Step 5: Full validation

**Verify**:

- `go test ./internal/review ./internal/receipt ./cmd` -> exit 0.
- `go test ./...` -> exit 0.
- `go vet ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Receipt finalization test for Claude-only provider.
- Receipt finalization test for mixed Codex + Claude provider.
- Receipt finalization test for missing provider evidence if not already covered by the missing-provider confidence test.
- Existing provider-label tests in `internal/review` stay green.

## Done criteria

- [ ] `receipt.Agent.Provider` matches the provider label derived from session events.
- [ ] Claude-only finalized receipts do not say `codex`.
- [ ] Mixed provider receipts identify both providers.
- [ ] Missing-provider receipts use `unknown` and keep provider confidence `none`.
- [ ] `go test ./...` exits 0.
- [ ] `go vet ./...` exits 0.
- [ ] `make verify` exits 0, or the operator records why the mutating gate was skipped.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- The receipt schema is expected to store machine provider IDs (`codex`, `claude`) instead of display labels, and no canonical mapping is documented.
- Fixing the provider value requires changing event ingestion or Claude hook payload contracts.
- Existing tests intentionally assert `receipt.Agent.Provider == "codex"` for missing-provider sessions.

## Maintenance notes

Future providers should add provider-label tests before adding receipt finalization behavior. Reviewer-facing display strings and signed receipt metadata should not drift; if machine-readable provider IDs are needed later, add a separate field rather than overloading this one silently.
