# Replay Evaluator Contract Progress

Plan source: [`PLAN.md`](./PLAN.md)

## Status

- Current status: Step 5 complete
- Current status: Step 6 complete
- Next step: Step 7
- Last updated: 2026-06-21T00:46:51Z
- Validation results:
  - `make fmt` (pass)
  - `go test ./internal/evidence ./internal/replay` (pass)
  - `make verify` (pass)

## Step Checklist

- [x] Step 0: Progress and Changelog Tracking Setup
- [x] Step 1: Define Explicit Replay Verification Verdicts
- [x] Step 2: Add Local Trust Policy Evaluation
- [x] Step 3: Harden Signer Material and Legacy Migration Semantics
- [x] Step 4: Add Evaluator Scoring Signals
- [x] Step 5: Add Quality Gate and Command Failure Evidence Schema
- [x] Step 6: Add Patch Semantic Summary
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

- Step 4 completed:
  - Added top-level `evaluator_signals` to replay output with deterministic command and file counts for production evaluator workflows.
  - Derived command counts from existing command-kind classification and `internal/commandrisk` signals, including failed statuses/exit codes and commit-like detection for unpaired command results.
  - Added file-change category counts for dependency, sensitive, production, test, and docs paths.
  - Added replay coverage for command attempts, command-result pairing edge cases, commit-like commands, and mixed file-category counting.
  - Commit: `Add replay evaluator signals`.

- Step 5 completed:
  - Added top-level `quality_gates` with stable gates (`format`, `lint`, `tests`, `race_tests`, `typecheck`, `security`, `coverage`, `build`, `smoke`, `verify`) and deterministic command/execution summaries including `status`, `commands`, `evidence_refs`, `last_exit_code`, and `confidence`.
  - Added top-level `failed_command_details` entries for failed commands, including command working directory, timestamp, exit code, redacted `failed_reason`, redacted `stderr_or_error_summary`, redacted `stdout_summary`, truncation flag, evidence refs, and confidence.
  - Added `cwd` and `time` fields to command records for richer failure context and paired command result metadata.
  - Added replay tests for successful `make verify` quality gates, failed `go test` gates and failed-command evidence, missing lint/typecheck gate runs, and sensitive output redaction in failure details.
  - Commit: `7d16758` (`Add replay patch summary`) and `63557de` (`Add replay quality gate tests`).

- Step 6 completed:
  - Added top-level `patch_summary` with file counts by category, additions/deletions, semantic changed-file entries, Go symbol hints, and test/production change relationship signals.
  - Added deterministic final-patch parsing that classifies test, docs, config, dependency, production, and generated/unknown paths without exposing raw diff bodies.
  - Added replay coverage for Go code changes, test/docs/dependency/config buckets, binary and rename diffs, malformed final patches, and JSON redaction of patch body content.
  - Added evidence helper coverage to lift repo-wide verification above the coverage gate without changing runtime behavior.
  - Commit: `7d16758` (`Add replay patch summary`)
