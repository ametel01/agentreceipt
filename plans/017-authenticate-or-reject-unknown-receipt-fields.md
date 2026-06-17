# Plan 017: Authenticate or reject unknown receipt fields

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 28911ab..HEAD -- internal/model/model.go internal/model/model_test.go internal/receipt/receipt.go internal/receipt/receipt_test.go docs/GITHUB_PR_WORKFLOW_DESIGN.md`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `28911ab`, 2026-06-17

## Why this matters

AgentReceipt's verifier currently recomputes the receipt hash from the decoded `model.Receipt` struct, not from the exact JSON bytes on disk. `model.DecodeReceipt` detects unknown top-level fields, but `receipt.Read` discards that information. As a result, a receipt can be modified by appending an extra top-level field and still verify as valid if all known fields remain unchanged. For a signed provenance artifact, either all JSON content must be covered by verification or unknown content must be rejected/warned as invalid.

## Current state

Relevant files:

- `internal/model/model.go` - receipt decoding and unknown-field detection.
- `internal/model/model_test.go` - confirms unknown top-level fields are reported by `DecodeReceipt`.
- `internal/receipt/receipt.go` - receipt read, verification, hash, and bundle verification.
- `internal/receipt/receipt_test.go` - tampering and signer-metadata verification tests.
- `docs/GITHUB_PR_WORKFLOW_DESIGN.md` - states CI verification must validate schemas.

Current excerpts:

```go
// internal/model/model.go:204
func DecodeReceipt(data []byte) (Receipt, []string, error) {
    var raw map[string]json.RawMessage
    if err := json.Unmarshal(data, &raw); err != nil {
        return Receipt{}, nil, fmt.Errorf("decode receipt object: %w", err)
    }
    known := map[string]struct{}{
        "schema_version":     {},
        "session_id":         {},
        "created_at":         {},
        "mode":               {},
        "agent":              {},
        "repo":               {},
        "summary":            {},
        "capture_confidence": {},
        "risk":               {},
        "verification":       {},
        "warnings":           {},
    }
    unknown := make([]string, 0)
    for key := range raw {
        if _, ok := known[key]; !ok {
            unknown = append(unknown, key)
        }
    }
    var receipt Receipt
    if err := json.Unmarshal(data, &receipt); err != nil {
        return Receipt{}, nil, fmt.Errorf("decode receipt: %w", err)
    }

    return receipt, unknown, nil
}
```

```go
// internal/receipt/receipt.go:260
func readReceiptPath(path string) (model.Receipt, error) {
    data, err := readFile(path)
    if err != nil {
        return model.Receipt{}, fmt.Errorf("read receipt json: %w", err)
    }
    receipt, _, err := model.DecodeReceipt(data)
    if err != nil {
        return model.Receipt{}, err
    }

    return receipt, nil
}
```

```go
// internal/receipt/receipt.go:370
func unsignedReceiptHash(receipt model.Receipt) (string, error) {
    receipt.Verification.ReceiptHash = ""
    receipt.Verification.Signature = ""
    data, err := model.MarshalCanonical(receipt)
    if err != nil {
        return "", err
    }

    return hashBytes(data), nil
}
```

```md
docs/GITHUB_PR_WORKFLOW_DESIGN.md:135
2. Validate JSON schemas and reject unknown critical receipt versions.
```

Repo conventions to follow:

- Receipt verification reports invalid results through `VerifyResult` booleans and `Warnings`, then callers return a non-zero error when `Valid` is false.
- Existing tamper tests in `internal/receipt/receipt_test.go` mutate fixture artifacts and then assert `Verify` returns `Valid: false`.
- Keep artifact verification deterministic and local; do not add network calls or external schema dependencies.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Model tests | `go test ./internal/model` | exit 0 |
| Receipt tests | `go test ./internal/receipt` | exit 0 |
| Focused full tests | `go test ./internal/model ./internal/receipt ./cmd` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Final gate | `make verify` | exit 0; note that it writes `coverage.out` and `./agentreceipt` |

## Scope

**In scope**:

- `internal/model/model.go`
- `internal/model/model_test.go`
- `internal/receipt/receipt.go`
- `internal/receipt/receipt_test.go`
- `docs/GITHUB_PR_WORKFLOW_DESIGN.md` only if behavior wording must be tightened

**Out of scope**:

- Changing the receipt schema version unless rejection behavior cannot be represented safely without it.
- Changing event-chain, manifest, final-patch, or signature algorithms.
- Changing portable signer behavior from plans 004 and 015.
- Adding hosted verification or GitHub Check behavior.

## Git workflow

- Branch: `advisor/017-receipt-unknown-fields`
- Commit message example from repo style: `fix: reject unauthenticated receipt fields`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add a failing regression test for unknown receipt fields

In `internal/receipt/receipt_test.go`, add a test near the existing receipt tampering tests. The test should:

1. Start and stop a temp session using the existing test helpers.
2. Finalize a receipt.
3. Read `layout.ReceiptJSON`.
4. Insert a new top-level field such as `"unauthenticated_note": "tampered"` before the final `}`.
5. Write the modified JSON back.
6. Run `Verify`.
7. Assert `result.Valid == false`, `result.ReceiptHash == false`, and `result.Warnings` contains a message naming unknown receipt fields.

Add the same coverage for `VerifyBundle` if the bundle helper can be reused without duplicating too much setup. The bundle verifier is especially important because it is the CI-oriented path.

**Verify**: `go test ./internal/receipt -run 'Unknown|Tamper|Bundle'` -> the new test fails before the implementation, proving the bug is covered.

### Step 2: Preserve unknown-field metadata in receipt reading

Change `internal/receipt/receipt.go` so the receipt reader used by `Verify` and `VerifyBundle` can report unknown fields. A small internal struct is acceptable, for example:

```go
type decodedReceipt struct {
    Receipt       model.Receipt
    UnknownFields []string
}
```

Keep `Read(layout storage.Layout) (model.Receipt, error)` as a public package helper if callers expect that signature, but make verification paths use the richer decoder. Sort unknown field names before putting them in warnings so test output is deterministic.

**Verify**: `go test ./internal/receipt -run 'Unknown|Tamper|Bundle'` -> unknown-field tests now pass or fail only on expected warning text.

### Step 3: Mark verification invalid when unknown fields exist

In both `Verify` and `VerifyBundle`, if decoded unknown fields are non-empty:

- append a warning such as `receipt contains unknown top-level fields: unauthenticated_note`;
- set `result.ReceiptHash = false` or add an equivalent validation boolean that contributes to `result.Valid == false`;
- ensure `result.Valid` is false.

Do not silently ignore unknown fields. Do not strip them and re-write the receipt. Verification should be read-only.

**Verify**: `go test ./internal/receipt` -> exit 0.

### Step 4: Keep model-level unknown-field round-trip behavior intentional

Do not remove `DecodeReceipt`'s ability to return unknown fields. Update `internal/model/model_test.go` only if the signature or ordering changes. The model package can continue to decode forward-compatible JSON; receipt verification is the layer that decides unknown fields are not acceptable in signed artifacts.

**Verify**: `go test ./internal/model` -> exit 0.

### Step 5: Align docs only if needed

If implementation changes the verification wording, update `docs/GITHUB_PR_WORKFLOW_DESIGN.md` to say local and bundle verification reject unknown top-level receipt fields. Keep docs concise and do not expand CI scope beyond current behavior.

**Verify**: `git diff --check -- internal/model/model.go internal/model/model_test.go internal/receipt/receipt.go internal/receipt/receipt_test.go docs/GITHUB_PR_WORKFLOW_DESIGN.md` -> no output, exit 0.

### Step 6: Full validation

Run the read-only checks first, then the full gate if the operator expects release-grade validation.

**Verify**:

- `go test ./...` -> exit 0.
- `go vet ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- New receipt test: local `Verify` rejects a receipt with an added unknown top-level field.
- New bundle test: `VerifyBundle` rejects a bundle receipt with an added unknown top-level field.
- Existing tests retained: typed-field tampering still invalidates `ReceiptHash`; signer metadata tampering still invalidates `Signature`.
- Existing model test retained: `DecodeReceipt` still reports unknown fields to callers.

## Done criteria

- [ ] `Verify` returns invalid for any receipt with unknown top-level fields.
- [ ] `VerifyBundle` returns invalid for any receipt with unknown top-level fields.
- [ ] Verification warnings name the unknown field names without exposing unrelated receipt content.
- [ ] Existing typed-field tampering tests still pass.
- [ ] `go test ./...` exits 0.
- [ ] `go vet ./...` exits 0.
- [ ] `make verify` exits 0, or the operator records why the mutating gate was skipped.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- The receipt decode path has already been changed to hash exact raw JSON bytes instead of typed structs.
- Unknown fields are now required for a documented forward-compatibility mechanism.
- Making unknown fields invalid requires a receipt schema version bump and the operator has not approved that compatibility change.
- Verification changes require touching signing key generation or event-log hashing.

## Maintenance notes

Future receipt schema additions must update the known-field list and add verification tests in the same change. Reviewers should scrutinize any change that makes verification tolerant of extra receipt content, because unsigned content in provenance artifacts is easy to misunderstand as verified.
