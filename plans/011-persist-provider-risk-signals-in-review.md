# Plan 011: Carry provider risk signals into final review and receipts

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat c5ab6b4..HEAD -- internal/commandrisk internal/provider/codex internal/review internal/receipt cmd/watch_render.go`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW-MED
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `c5ab6b4`, 2026-06-17

## Why this matters

The live watch stream uses `commandrisk` to detect high-risk commands such as secret reads, package publishes, cloud mutations, destructive git operations, and database migrations. The final review and receipt rebuild risk from a smaller hard-coded pattern set, so important live risk context can disappear from the durable PR artifact. The receipt should preserve the same deterministic risk evidence that the live stream saw.

## Current state

Relevant files:

- `internal/commandrisk/classifier.go` - full deterministic command-risk rule set.
- `internal/provider/codex/codex.go` - builds `RiskSignals` during parsing.
- `cmd/watch_render.go` - uses `RiskSignals` for live watch output.
- `internal/review/review.go` - builds final review risk and currently ignores provider risk signal events/traces.
- `internal/receipt/receipt.go` - copies review risk into finalized receipts.

Current excerpts:

```go
// internal/provider/codex/codex.go:381
func (r *ParseResult) addRiskSignals(options ParseOptions, command string) {
    for _, classification := range commandrisk.Classify(command) {
        r.RiskSignals = append(r.RiskSignals, RiskSignal{ ... })
    }
}
```

```go
// cmd/watch_render.go:96
riskSignals := riskSignalsByLine(result.RiskSignals)
```

```go
// internal/review/review.go:89
var commandKindPatterns = []struct {
    kind    string
    pattern *regexp.Regexp
}{
    {kind: "network", pattern: regexp.MustCompile(`\b(curl|wget|ssh|nc|aws|gcloud)\b`)},
    {kind: "destructive", pattern: regexp.MustCompile(`\b(rm|dd|mkfs|shutdown|reboot)\b`)},
}
```

```go
// internal/review/review.go:896
for _, command := range summary.DetectedCommands {
    if command.Kind == "network" || command.Kind == "destructive" {
        result.Level = maxRisk(result.Level, model.RiskHigh)
    }
}
```

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Command risk tests | `go test ./internal/commandrisk` | exit 0 |
| Codex parser tests | `go test ./internal/provider/codex` | exit 0 |
| Review tests | `go test ./internal/review` | exit 0 |
| Receipt tests | `go test ./internal/receipt` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Final gate | `make verify` | exit 0 |

## Scope

**In scope**:

- `internal/provider/codex/codex.go`
- `internal/provider/codex/codex_test.go`
- `internal/review/review.go`
- `internal/review/review_test.go`
- `internal/receipt/receipt_test.go`
- `internal/model/model.go` only if a small provider-risk summary type is necessary

**Out of scope**:

- Rewriting `commandrisk` rules.
- Adding AI scoring or model trust scores.
- Changing live watch rendering except to keep tests aligned.
- Changing receipt signature semantics.

## Git workflow

- Branch: `advisor/011-provider-risk-in-review`
- Commit message example: `fix: persist provider risk signals in reviews`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Store risk signal evidence in normalized events

Update Codex parsing so risk classifications are available to review without reading side trace files. The simplest shape is to include risk signals on the `provider.command` event payload alongside the `tool_call`, for example `risk_signals: []RiskSignal`. Preserve the existing trace files.

Do not store duplicate unredacted command strings. Reuse redacted command text already present in the risk signal.

**Verify**: `go test ./internal/provider/codex` -> exit 0.

### Step 2: Teach review to read provider risk signals

Update `internal/review/review.go` so `summarize` or `risk` collects provider risk signal payloads and turns them into `model.RiskReason` entries. Preserve existing sensitive path, dependency, warning, and command-status behavior.

Required mapping:

- high provider risk -> high review risk
- medium provider risk -> at least medium review risk
- low provider risk -> low review risk only when useful; avoid noise if existing behavior intentionally suppresses low-risk script-runner signals

Use deterministic codes, such as `provider_risk_secret_access` or `provider_risk_cloud_or_deploy_mutation`.

**Verify**: `go test ./internal/review` -> exit 0.

### Step 3: Add receipt regression coverage

Add or update receipt tests so finalized receipts include the provider risk reason when a session contains a command such as `cat .env` or `git push --force`. The test must prove the risk appears in `receipt.Risk.Reasons`, not just the live watch renderer.

**Verify**: `go test ./internal/receipt` -> exit 0.

### Step 4: Full validation

**Verify**:

- `go test ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Parser test: command with `cat .env` creates a normalized provider event with a risk signal and redacted command text.
- Review test: provider risk signal becomes a review risk reason.
- Receipt test: finalized receipt keeps the same risk reason.
- Existing live watch risk tests must still pass.

## Done criteria

- [ ] Provider risk signals are available from `events.jsonl`.
- [ ] Final review includes high/medium provider risk reasons beyond the current network/destructive regex set.
- [ ] Final receipt includes the same durable risk reason.
- [ ] No unredacted command output is introduced.
- [ ] `go test ./...` exits 0.
- [ ] `make verify` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Adding provider risk evidence requires a receipt schema migration.
- Privacy Plan 010 changed provider event payload shape in a conflicting way; adapt only if the new shape is obvious and tested.
- Risk signal persistence would store raw prompts or raw command output.

## Maintenance notes

Provider integrations should emit provider-neutral risk evidence so review and receipt logic do not need provider-specific branches. Keep the risk model deterministic; do not add model or developer scoring.
