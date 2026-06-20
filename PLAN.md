# Implementation Plan

## Source Documents
- Path: Inline prompt from current conversation
  - Role: Primary implementation brief
  - Summary: Fix verifier replay so it reports factual session evidence only, rebuilds the CLI before judging output, parses command statuses correctly when nested exit markers appear, derives replay files from `diffs/final.patch`, excludes review/evaluation conclusions from replay gaps, and exposes component-level verification details.

## Goals
- Make `agentreceipt replay` a factual, artifact-only session reconstruction surface for evaluator agents.
- Ensure replay command status and exit code fields match the actual tool execution result, including Codex outputs with nested exit markers.
- Ensure replay file lists reflect the final patch even when filesystem watcher events are missing.
- Keep replay gaps limited to integrity and evidence availability issues, not quality-policy judgments.
- Make verification failures actionable by exposing component validity and signature failure reason.
- Ensure the locally built `./agentreceipt` binary reflects source changes before replay behavior is evaluated.

## Non-Goals
- Do not reintroduce replay risk scoring, quality scoring, or policy evaluation.
- Do not rerun recorded commands, reapply patches, call models, or mutate workspace state during replay.
- Do not change receipt signing semantics beyond reporting clearer verification status.
- Do not remove review-side risk, lint, test, or typecheck guidance; those remain review/evaluator concerns.
- Do not migrate or rewrite historical session artifacts.

## Assumptions and Open Questions
- Assumption: `replay` may keep `summary.final_risk` temporarily for schema compatibility, but it should remain neutral (`info`) unless a future schema removes it.
- Assumption: `verifier_tasks` should either mirror factual replay gaps or become empty; this plan uses factual gaps only to avoid evaluator conclusions.
- Assumption: A final-patch-only file should use `diffs/final.patch` as its evidence reference when no `fs.change` event exists.
- Open question: Whether the replay JSON schema can add fields without a schema version bump. Conservative approach: add fields and keep existing fields until a deliberate schema migration is planned.
- Open question: Whether legacy receipts without embedded signer public keys should be reported as `legacy_missing_embedded_signer` or a more general signature error. The implementation should choose a stable machine-readable error code.

## Quality Gates
- Setup status: Existing gates are defined in `Makefile` and enforced by `.github/workflows/ci.yml` through `make verify`.
- Baseline command: `make verify`
- Format command: `make fmt-check`
- Lint command: `make lint`
- Test command: `make test`
- Additional gates: `make test-race`, `make security`, `make coverage`, `make build`, `make smoke`, `make verify`
- Notes: `make tools` installs local lint/security tools if `.tools/bin` is missing. If baseline `make verify` fails before implementation, record the exact failure in `PROGRESS.md` and distinguish it from new regressions.

## Progress Tracking
- File: `PROGRESS.md`
- Requirement: Create or refresh `PROGRESS.md` before any implementation work begins.
- Update rule: After each step is completed, update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.

## Changelog Tracking
- File: `CHANGELOG.md`
- Standard: Keep a Changelog 1.0.0, <https://keepachangelog.com/en/1.0.0/>
- Requirement: Create `CHANGELOG.md` before any implementation work begins if missing; otherwise preserve the existing Keep a Changelog structure.
- Initial content: Include `# Changelog`, the standard preamble, and an `## [Unreleased]` section.
- Update rule: After each step is completed and validated, update `CHANGELOG.md` with human-readable notable changes under the appropriate `Unreleased` change-type headings before creating that step's commit.

## Incremental Steps

### Step 0: Progress and Changelog Tracking Setup
Goal: Create or refresh durable progress and changelog tracking for this replay-fix plan.

Depends on:
- None

Changes:
- Update `PROGRESS.md` in the project root for this plan.
- Add the plan title/source, step checklist, current status, and short update log.
- Document that `PROGRESS.md` must be updated after every completed step.
- Confirm `CHANGELOG.md` exists and follows Keep a Changelog 1.0.0 structure.
- If `CHANGELOG.md` is missing or malformed, create/fix it before any implementation work begins.

Acceptance Criteria:
- `PROGRESS.md` references this replay-fix plan and contains every step below as an unchecked checklist item.
- `CHANGELOG.md` has `# Changelog`, the standard preamble, and `## [Unreleased]`.

Validation:
- Run `test -f PROGRESS.md`
- Run `test -f CHANGELOG.md`
- Run `rg -n "Replay Fix|Step 1|Step 6" PROGRESS.md`
- Run `rg -n "^# Changelog|^## \\[Unreleased\\]" CHANGELOG.md`

Progress:
- Mark Step 0 complete in `PROGRESS.md`, record validation results, set current status, and identify Step 1 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for establishing replay-fix progress tracking if this is notable and not already represented.

Commit:
- `docs: start replay fix tracking`

### Step 1: Baseline Replay Characterization and Binary Rebuild
Goal: Ensure evaluator observations are based on a freshly built binary and capture current replay defects with focused regression tests.

Depends on:
- Step 0

Changes:
- Run `make build` before replaying any session so `./agentreceipt` reflects current source.
- Re-run `./agentreceipt replay --session ar_ses_1781632141211056000_114075a87efc` and record the relevant before/after observations in `PROGRESS.md`.
- Add or update characterization tests in `internal/replay/replay_test.go`, `internal/provider/codex/codex_test.go`, or `cmd/root_test.go` for currently observed behavior:
  - stale `risk_signals` must not appear in replay output after rebuild.
  - nested command output with final non-zero process exit should not be reported as success.
  - replay should not report review-only gaps such as missing lint once factual gaps are separated in a later step.
- Avoid relying on the historical session artifact as the only test fixture; use focused synthetic events where possible.

Acceptance Criteria:
- `make build` refreshes `./agentreceipt`.
- A replay run from the rebuilt binary no longer includes stale removed fields from prior source revisions.
- Characterization tests fail for at least one known weak point before its implementation fix, or are added alongside the fix if the current tree already partially addressed it.

Validation:
- Run `make fmt-check`
- Run `make lint`
- Run `make test`
- Run `make test-race`
- Run `make security`
- Run `make coverage`
- Run `make build`
- Run `make smoke`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with baseline replay observations, validation results, commit reference if available, current status, and Step 2 as next.

Changelog:
- Add a `Changed` or `Fixed` entry under `## [Unreleased]` only if test or behavior changes are committed.

Commit:
- `test: characterize replay evaluator defects`

### Step 2: Correct Codex Command Status Parsing
Goal: Make command result status and exit code reflect the actual command execution result when Codex output contains nested exit markers.

Depends on:
- Step 1

Changes:
- Update `internal/provider/codex/codex.go` in `commandStatus`.
- Prefer `Process exited with code N` markers over wrapper-level `Exit code: N` markers when both are present.
- If multiple equivalent markers exist, use the last relevant marker so summaries that include nested command output do not mask final failure.
- Preserve current behavior for simple `Exit code: 0`, `Exit code: 1`, empty output, and failure-marker-only output.
- Add regression tests in `internal/provider/codex/codex_test.go` for:
  - `Exit code: 0` followed by `Process exited with code 2` returns `failed`, exit code `2`.
  - `Exit code: 0` alone returns `success`, exit code `0`.
  - no marker with non-empty output remains `success` unless failure markers are present.
- Verify replay pairing still surfaces the corrected status through `internal/replay`.

Acceptance Criteria:
- Replay commands with nested non-zero process exits are reported as `status: "failed"` with the matching `exit_code`.
- Existing simple command-output status tests continue to pass.
- No command status is inferred from risk, review policy, or evaluator judgment.

Validation:
- Run `make fmt-check`
- Run `make lint`
- Run `make test`
- Run `make test-race`
- Run `make security`
- Run `make coverage`
- Run `make build`
- Run `make smoke`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with completed scope, validation results, commit reference if available, current status, and Step 3 as next.

Changelog:
- Add a `Fixed` entry under `## [Unreleased]` describing corrected Codex command-result status parsing.

Commit:
- `fix: parse nested Codex command exits correctly`

### Step 3: Derive Replay Files From Final Patch
Goal: Make replay file output reflect the final artifact patch even when filesystem watcher events are absent or incomplete.

Depends on:
- Step 2

Changes:
- Update `internal/replay/replay.go`.
- Extend `buildFiles` or add a helper to parse `diffs/final.patch` diff headers into repo-relative file paths.
- Union file paths from:
  - `evidenceSummary.ChangedFiles`
  - `fs.change` events
  - parsed `diff --git a/<path> b/<path>` entries from final patch
- For final-patch-only files:
  - set `InFinalPatch: true`
  - use action from diff metadata when practical (`add`, `modify`, `delete`, `rename`), otherwise default to `modify`
  - set `EvidenceRefs` to a stable final patch reference such as `diffs/final.patch`
  - keep `Sensitive` and `Dependency` classification by reusing existing changed-file classification helpers where available, or add a replay-local classifier matching existing config behavior.
- Change replay `summary.changed_file_count` to `len(files)`.
- Add tests in `internal/replay/replay_test.go` for:
  - final patch files appear even when no `fs.change` events exist.
  - files with both event and final patch evidence are deduplicated.
  - deleted or renamed diff headers are parsed safely.

Acceptance Criteria:
- A session with non-empty `diffs/final.patch` does not report `files: []`.
- `changed_file_count` matches the emitted `files` array length.
- File output remains deterministic and sorted.

Validation:
- Run `make fmt-check`
- Run `make lint`
- Run `make test`
- Run `make test-race`
- Run `make security`
- Run `make coverage`
- Run `make build`
- Run `make smoke`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with completed scope, validation results, commit reference if available, current status, and Step 4 as next.

Changelog:
- Add a `Fixed` entry under `## [Unreleased]` describing replay file reconstruction from final patches.

Commit:
- `fix: derive replay files from final patch`

### Step 4: Limit Replay Gaps to Factual Evidence Issues
Goal: Remove review/evaluation conclusions from replay gaps and verifier tasks.

Depends on:
- Step 3

Changes:
- Update `internal/replay/replay.go` so replay no longer calls `evidence.Gaps` directly if that function includes review-policy conclusions.
- Add a replay-specific factual gap builder that includes only:
  - unreadable or missing required artifacts
  - event-chain replay errors
  - signature/hash verification failures
  - unknown command status for observed command attempts
  - unpaired command result events
  - non-finalized session state
  - missing provider tool events only if it means the session lacks sequence evidence, not as a quality-policy warning
- Exclude evaluator judgments such as:
  - no lint command detected
  - no test command detected
  - no typecheck command detected
  - review focus/risk prompts
- Set `VerifierTasks` to the factual gaps or an empty array; do not add policy recommendations.
- Update `internal/replay/replay_test.go` to assert missing lint/test/typecheck are absent from replay gaps while still present in review behavior tests where appropriate.

Acceptance Criteria:
- Replay output does not include `No lint command detected.` for sessions that simply lack lint evidence.
- Integrity and evidence-completeness issues remain visible.
- Review output keeps quality guidance unchanged.

Validation:
- Run `make fmt-check`
- Run `make lint`
- Run `make test`
- Run `make test-race`
- Run `make security`
- Run `make coverage`
- Run `make build`
- Run `make smoke`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with completed scope, validation results, commit reference if available, current status, and Step 5 as next.

Changelog:
- Add a `Changed` entry under `## [Unreleased]` describing replay gaps as factual evidence issues only.

Commit:
- `fix: keep replay gaps factual`

### Step 5: Add Component-Level Verification Details
Goal: Make replay verification failures actionable without requiring a human to infer which artifact or signature component failed.

Depends on:
- Step 4

Changes:
- Update `internal/replay.Verification` in `internal/replay/replay.go` to include component booleans and stable error fields while preserving existing hash fields:
  - `event_chain_valid`
  - `final_patch_hash_valid`
  - `manifest_hash_valid`
  - `receipt_hash_valid`
  - `signature_error,omitempty`
  - optional stable `signature_error_code,omitempty`
- Update `buildVerification` to populate these fields from `receipt.VerifyBundle` results and warnings.
- If `receipt.VerifyBundle` does not expose enough detail, update `internal/receipt.VerifyResult` to carry stable component error details without changing successful verification behavior.
- Detect legacy/unportable signature cases where `receipt.json` contains a signature but lacks embedded signer public key/key ID, and report a specific signature error.
- Add tests in `internal/replay/replay_test.go` and/or `internal/receipt/receipt_test.go` for:
  - valid bundle has all component booleans true.
  - tampered final patch sets only final patch validity false where practical.
  - legacy missing embedded signer reports a specific signature error in replay.

Acceptance Criteria:
- Replay verification tells evaluators exactly which component failed.
- Existing `verification.valid` semantics remain true only when all required integrity and signature components pass.
- Legacy signature portability problems are distinguishable from tampering.

Validation:
- Run `make fmt-check`
- Run `make lint`
- Run `make test`
- Run `make test-race`
- Run `make security`
- Run `make coverage`
- Run `make build`
- Run `make smoke`
- Run `make verify`

Progress:
- Step 5 implementation is complete: replay verification now exposes component-level booleans and stable signature error codes, and tests cover valid/tampered and legacy-signer scenarios.
- Update `PROGRESS.md` with completed scope, validation results, commit reference if available, and Step 6 as next.

Changelog:
- Added entry is now present in `CHANGELOG.md` describing component-level replay verification fields and signature error codes.

Commit:
- `feat: report replay verification components`

### Step 6: Document and Smoke-Test Evaluator Replay Contract
Goal: Lock in replay as a factual evaluator input and verify the rebuilt CLI emits the intended shape.

Depends on:
- Step 5

Changes:
- Update `README.md`, `docs/TECH_SPEC.md`, and any replay-specific docs to clarify:
  - replay reconstructs sequence and artifact facts only.
  - replay does not score the agent, classify risk, or decide quality adequacy.
  - evaluator agents should infer review conclusions from command/files/artifact facts.
  - `make build` or `make verify` refreshes `./agentreceipt` before local replay checks.
- Update `scripts/smoke.sh` if needed to assert:
  - replay JSON omits `risk_signals`
  - replay gaps exclude review-only lint/test/typecheck conclusions
  - replay includes final patch files for a smoke session with changes
  - replay verification exposes component validity fields
- Rebuild `./agentreceipt` and re-run the original replay command:
  - `./agentreceipt replay --session ar_ses_1781632141211056000_114075a87efc`
- Record in `PROGRESS.md` whether the historical session now shows corrected command statuses, final patch files, factual gaps, and detailed verification state.

Acceptance Criteria:
- Documentation matches the final replay behavior.
- Smoke coverage prevents regression to evaluator/scoring output.
- The named historical session is useful as a factual review artifact even if its signature remains invalid because of legacy signer portability.

Validation:
- Run `make fmt-check`
- Run `make lint`
- Run `make test`
- Run `make test-race`
- Run `make security`
- Run `make coverage`
- Run `make build`
- Run `make smoke`
- Run `make verify`
- Run `./agentreceipt replay --session ar_ses_1781632141211056000_114075a87efc`

Progress:
- Update `PROGRESS.md` with completed scope, validation results, final replay observations, commit reference if available, current status, and `Plan complete`.

Changelog:
- Add `Changed` and/or `Fixed` entries under `## [Unreleased]` for documented evaluator replay contract and smoke coverage.

Commit:
- `docs: clarify factual replay contract`
