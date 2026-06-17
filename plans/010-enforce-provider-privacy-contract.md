# Plan 010: Enforce provider privacy before storing Codex evidence

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat c5ab6b4..HEAD -- internal/config internal/provider/codex cmd/root.go cmd/root_test.go docs/TECH_SPEC.md docs/CLAUDE_PROVIDER_DESIGN.md README.md`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `c5ab6b4`, 2026-06-17

## Why this matters

AgentReceipt's docs and config promise local, privacy-preserving evidence. Defaults say prompts and raw tool outputs should not be stored, but Codex parsing currently stores raw-ish provider payloads and command output into normalized events and trace files with only substring redaction. This is especially important before Claude hook work, because Claude design explicitly says prompt text should not be stored in normalized events by default.

## Current state

Relevant files:

- `internal/config/config.go` - privacy and capture defaults.
- `internal/provider/codex/codex.go` - Codex parser, redaction, and trace writing.
- `cmd/root.go` - calls parser from `import`, `review --codex-jsonl`, and `start --watch`.
- `docs/TECH_SPEC.md` and `docs/CLAUDE_PROVIDER_DESIGN.md` - privacy contract.

Current excerpts:

```go
// internal/config/config.go:43
type Privacy struct {
    RedactSecrets       bool `yaml:"redact_secrets" json:"redact_secrets"`
    StorePrompts        bool `yaml:"store_prompts" json:"store_prompts"`
    StoreRawToolOutputs bool `yaml:"store_raw_tool_outputs" json:"store_raw_tool_outputs"`
    MaxBlobBytes        int  `yaml:"max_blob_bytes" json:"max_blob_bytes"`
}
```

```go
// internal/config/config.go:73
Privacy: Privacy{
    RedactSecrets:       true,
    StorePrompts:        false,
    StoreRawToolOutputs: false,
    MaxBlobBytes:        200000,
},
```

```go
// internal/provider/codex/codex.go:344
r.Events = append(r.Events, model.Event{
    Type: "provider.command_result",
    Payload: map[string]any{
        "line_no":        r.LineCount,
        "command_result": commandEvent,
    },
})
```

```go
// internal/provider/codex/codex.go:478
Payload: map[string]any{
    "line_no":      lineNumber,
    "record_type":  recordType,
    "payload_type": payloadType,
    "raw":          redactMap(raw, options.MaxOutputBytes),
},
```

Documented constraints:

- `docs/TECH_SPEC.md:140` sets `redact_secrets: true`, `store_prompts: false`, and `store_raw_tool_outputs: false`.
- `docs/CLAUDE_PROVIDER_DESIGN.md:91` says normalized event payloads must be redacted before they enter `events.jsonl`.
- `docs/CLAUDE_PROVIDER_DESIGN.md:94` says prompt text should not be stored in normalized events by default.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Codex parser tests | `go test ./internal/provider/codex` | exit 0 |
| CLI tests | `go test ./cmd` | exit 0 |
| Config tests | `go test ./internal/config` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Final gate | `make verify` | exit 0 |

## Scope

**In scope**:

- `internal/provider/codex/codex.go`
- `internal/provider/codex/codex_test.go`
- `internal/config/config.go`
- `internal/config/config_test.go`
- `cmd/root.go`
- `cmd/root_test.go`
- `docs/TECH_SPEC.md`
- `README.md`

**Out of scope**:

- Claude hook implementation.
- Hosted or cloud upload behavior.
- Changing receipt signature fields.
- A broad DLP engine.

## Git workflow

- Branch: `advisor/010-provider-privacy-contract`
- Commit message example: `fix: enforce provider privacy defaults`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add parser privacy options

Extend `codex.ParseOptions` so the parser receives the relevant config-derived privacy choices:

- `RedactSecrets`
- `StorePrompts`
- `StoreRawToolOutputs`
- `MaxOutputBytes`

Default behavior must preserve the documented default privacy posture when callers do not explicitly pass options: redact secrets, do not store prompts, and do not store raw tool outputs.

**Verify**: `go test ./internal/provider/codex` -> exit 0.

### Step 2: Sanitize normalized events before append

Update `providerEvent`, `consumeToolCall`, and `consumeCommandOutput` so `events.jsonl` does not retain prompt/message text or raw tool output when the corresponding config field is false.

Target behavior:

- Command text may be retained because review depends on it, but secrets inside it must be redacted when `RedactSecrets` is true.
- Command output should be replaced by a placeholder or metadata when `StoreRawToolOutputs` is false. Keep status, exit code, truncation flag, and failure reason.
- User/assistant/developer/system message text should be omitted or replaced with metadata when `StorePrompts` is false.
- Raw provider records should not be stored in normalized events by default.

**Verify**: add parser tests, then run `go test ./internal/provider/codex` -> exit 0.

### Step 3: Thread config into all Codex parse call sites

Update these call sites so loaded config controls parser behavior:

- `import codex-jsonl`
- `review --codex-jsonl`
- `start --watch`

Use the existing `managerFromCommand` path where possible. For `start --watch`, make sure options passed into `codex.TailFile` include the same privacy behavior as explicit import.

**Verify**: `go test ./cmd ./internal/provider/codex` -> exit 0.

### Step 4: Update docs only where behavior is clarified

If implementation details changed user-visible behavior, update README or TECH_SPEC near the privacy wording. Keep wording concise and factual.

**Verify**: `git diff --check -- README.md docs/TECH_SPEC.md internal/provider/codex/codex.go cmd/root.go` -> no output, exit 0.

### Step 5: Full validation

**Verify**:

- `go test ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Add a Codex parser test with a `user_message` or assistant message and assert prompt text is absent from `result.Events` by default.
- Add a command-output test and assert raw stdout content is absent by default while `status` and `exit_code` remain.
- Add a test that explicit opt-in preserves raw output where the config allows it.
- Keep existing secret redaction tests and add one nested raw-map case that would fail if `redactMap` misses nested prompt/output fields.

## Done criteria

- [ ] Default normalized events do not store prompt/message text.
- [ ] Default normalized events do not store raw command output.
- [ ] Secret redaction still applies to retained command text and metadata.
- [ ] All Codex parse call sites receive config-derived privacy options.
- [ ] `go test ./...` exits 0.
- [ ] `make verify` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Review correctness currently requires raw stdout rather than status/exit metadata.
- Privacy enforcement requires changing receipt schema or signatures.
- The desired default is disputed by docs after drift check.
- A needed redaction rule would require embedding real secret values in tests or plans.

## Maintenance notes

Future provider integrations should share this privacy contract. Claude work must not start until normalized events have tests proving prompt and raw-output defaults.
