# Replay Evaluator Contract Progress

Plan source: [`PLAN.md`](./PLAN.md)

## Status

- Current status: Step 3 complete
- Next step: Step 4
- Last updated: 2026-06-21T00:17:18Z
- Validation results:
  - `make fmt`
  - `go test ./internal/replay ./cmd` (pass)
  - `go test ./internal/trust ./internal/replay ./cmd` (pass)
  - `make verify` (fails at coverage gate: `go tool cover -func=coverage.out | awk '/total:/ { if ($3+0 < 80.0) exit 1 }`)

## Step Checklist

- [x] Step 0: Progress and Changelog Tracking Setup
- [x] Step 1: Define Explicit Replay Verification Verdicts
- [x] Step 2: Add Local Trust Policy Evaluation
- [x] Step 3: Harden Signer Material and Legacy Migration Semantics
- [ ] Step 4: Add Evaluator Scoring Signals
- [ ] Step 5: Add Quality Gate and Command Failure Evidence Schema
- [ ] Step 6: Add Patch Semantic Summary
- [ ] Step 7: Add Policy Checks and Review Focus
- [ ] Step 8: Add Privacy Report, Claim Confidence, and Outcome Classification
- [ ] Step 9: Document the Production Replay Evaluator Contract

## Implementation Notes

- Keep additive replay report fields for compatibility with existing consumers.
- Preserve existing `Valid`/`SignatureValid` semantics in replay JSON for compatibility while introducing new verdict fields.
- Keep replay as an evidence-only report and run only local, deterministic analyses.

## Completed Step Notes

- Step 1 completed:
  - Extended `internal/replay/replay.go` verification model with additive integrity/authenticity/trust/policy/vs verdict fields and component checks.
  - Added helper-backed characterizations for intact verification, legacy missing signer behavior, and signature mismatch with intact hashes.
  - Updated replay tests to assert `integrity_valid`, `authenticity_status`, `overall_verdict`, and per-component verification reasons/results.
  - Commit: `Split replay verification verdicts`.

- Step 2 completed:
  - Added `internal/trust/trust.go` with trusted signer key ID normalization, extraction, and policy evaluation.
  - Extended config with `trust.trusted_signer_key_ids` and added validation for malformed key IDs.
  - Added `--trusted-signer-key-id` replay flag with config merge semantics and CLI-level input validation.
  - Wired trust-policy results through replay verification (`trust_status`, `signer_trusted`, `policy_valid`), including trusted vs untrusted behavior when signatures are valid.
  - Added trust-policy characterization tests in `internal/trust`, `internal/replay`, and `cmd` test suites, including malformed-key failure handling.
  - Commit: `Add replay signer trust policy`.

- Step 3 completed:
  - Confirmed receipt finalization always writes embedded signer metadata (`signer_public_key`, `signer_key_id`, `signature_algorithm`, `signature`), including local report hash/signature material.
  - Confirmed bundle verification path uses embedded signer material and reports explicit legacy-missing-key failures when metadata is absent.
  - Confirmed replay verification reuses `receipt.VerifyBundle`, so embedded signer metadata flows into replay authenticity checks without requiring local private key state.
  - Kept behavior consistent with unchanged legacy semantics: missing embedded signer surfaces as unauthentic but intact integrity.
  - Commit: `Harden replay signer portability`.
