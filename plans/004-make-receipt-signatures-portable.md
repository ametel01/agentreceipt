# Plan 004: Make receipt signatures self-contained and portable

> **Executor instructions**: Follow this plan exactly. Run every verification command. If a STOP condition occurs, stop and report. Update `plans/README.md` when done.
>
> **Drift check (run first)**: `git diff --stat f9e7997..HEAD -- internal/receipt/receipt.go internal/receipt/receipt_test.go internal/model/model.go internal/model/model_test.go internal/signing/signing.go internal/signing/signing_test.go README.md docs/TECH_SPEC.md`
> If in-scope files changed, compare the excerpts below against live code before proceeding.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `f9e7997`, 2026-06-17

## Why this matters

Receipts are marketed as signed and verifiable, but verification currently depends on whatever default public key exists on the verifier's machine. If the signer rotates keys or a reviewer receives artifacts without the original local key directory, verification can fail even though the receipt contains enough signature data to identify the signer. The receipt should carry signer public key or key fingerprint metadata so it is portable and reviewable.

## Current state

Current excerpts:

```go
// internal/receipt/receipt.go:89
keypair, err := signing.LoadOrCreateDefault(options.KeyDir)
receipt.Verification.Signature = signing.Sign(keypair.PrivateKey, signPayload)
```

```go
// internal/receipt/receipt.go:156
publicKey, publicPath, err := signing.LoadDefaultPublic(options.KeyDir)
...
result.Signature = signing.Verify(publicKey, payload, signature)
result.SignedBy = publicPath
```

```go
// internal/model/model.go:154
type Verification struct {
    EventChainHash     string `json:"event_chain_hash"`
    DiffHash           string `json:"diff_hash"`
    ManifestHash       string `json:"manifest_hash"`
    ReceiptHash        string `json:"receipt_hash"`
    SignatureAlgorithm string `json:"signature_algorithm"`
    Signature          string `json:"signature"`
    Valid              bool   `json:"valid"`
}
```

Repo conventions:
- Receipt JSON is decoded with `model.DecodeReceipt`; unknown fields are tolerated and reported.
- Tests tamper receipt and patch artifacts to prove verification failure.
- Use Ed25519 and base64-encoded key material.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Receipt tests | `go test ./internal/receipt ./internal/model ./internal/signing` | exit 0 |
| Command tests | `go test ./cmd` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Final gate | `make verify` | exit 0 |

## Scope

**In scope**:
- `internal/model/model.go`
- `internal/model/model_test.go`
- `internal/receipt/receipt.go`
- `internal/receipt/receipt_test.go`
- `internal/signing/signing.go`
- `internal/signing/signing_test.go`
- `docs/TECH_SPEC.md` or `README.md` only if schema/user behavior is documented there

**Out of scope**:
- Trust-on-first-use key management.
- Remote certificate transparency, notarization, or hosted verification.
- Changing the hash chain algorithm.

## Git workflow

- Branch: `advisor/004-portable-receipts`
- Commit message example: `Make receipt signatures portable`.
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add signer metadata to the verification schema

Extend `model.Verification` with fields such as:

- `SignerPublicKey string json:"signer_public_key,omitempty"` for base64 Ed25519 public key;
- `SignerKeyID string json:"signer_key_id,omitempty"` for a stable hash/fingerprint of the public key.

Use names that fit existing JSON style. Update `DecodeReceipt` known fields only if needed; because fields are nested in `verification`, top-level known-field handling may not need changes.

**Verify**: `go test ./internal/model` -> exit 0.

### Step 2: Populate signer metadata during finalize

In `receipt.Finalize`, after loading the keypair, set signer metadata before computing the final signature payload. Decide whether signer metadata is included in the signed payload:

- Recommended: include signer key ID in the unsigned receipt hash and signature payload so tampering is detected.
- Keep the actual signature field excluded from `unsignedReceiptHash`, as it is today.

**Verify**: `go test ./internal/receipt -run TestFinalizeVerifyAndExportReceipt` -> exit 0.

### Step 3: Verify using embedded signer metadata

Update `receipt.Verify`:

- Prefer the embedded signer public key when present.
- Fall back to the default local public key only for old receipts that lack embedded metadata.
- Set `SignedBy` to a useful label such as `embedded:<key-id>` for embedded verification and the file path for legacy local verification.

Add tests:

- verification succeeds when only embedded public key is available;
- tampering signer public key or key ID invalidates verification;
- legacy receipts without embedded key still verify via `KeyDir` if possible.

**Verify**: `go test ./internal/receipt ./internal/signing` -> exit 0.

### Step 4: Update docs if necessary

If README or TECH_SPEC describes receipt signature fields, update the closest authoritative section with the new signer metadata. Keep wording factual and concise.

**Verify**:
- `git diff --check -- README.md docs/TECH_SPEC.md internal/model/model.go internal/receipt/receipt.go` -> no output.
- `go test ./...` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Add receipt verification tests for embedded public key success.
- Add tamper tests for signer metadata.
- Keep existing patch/receipt tamper tests passing.

## Done criteria

- [ ] New receipts contain signer public key or key ID metadata.
- [ ] Verification does not require local key directory for new receipts.
- [ ] Old receipts can still verify with local public key.
- [ ] Tampering signer metadata fails verification.
- [ ] `go test ./...` and `make verify` pass.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Adding metadata requires a breaking schema version bump.
- There is disagreement about whether public keys should be stored in receipts.
- Verification semantics become ambiguous between embedded and local keys.

## Maintenance notes

This plan makes receipts easier to share, but it does not establish identity trust. A reviewer can verify artifact integrity, not that a public key belongs to a specific human or machine.
