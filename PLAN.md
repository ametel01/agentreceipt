# Implementation Plan

## Source Documents

- Path: `/Users/alexmetelli/source/agentreceipt/evaluator-loop-spec.md.`
  - Role: Primary evaluator-loop specification.
  - Summary: Requires replay verification to distinguish internal integrity from signer authenticity, fix or clearly report missing embedded signer public keys, add trust-policy semantics, handle legacy receipts, and make replay output useful as a production-grade evaluator contract with quality gates, policy checks, scoring signals, command failure evidence, patch summaries, review focus, confidence, privacy reporting, and outcome classification.

## Goals

- Make `agentreceipt replay` cryptographically explicit by splitting integrity, authenticity, trust, policy, and overall verdicts.
- Preserve compatibility with existing replay fields while adding a clearer evaluator-oriented contract.
- Ensure new finalized receipts and replay bundles expose enough signer material for portable signature verification, and make legacy missing-signer receipts explicitly `integrity_valid=true` but `authenticity=unverifiable`.
- Add a trust-policy layer so production agent loops can distinguish "signature matches embedded key" from "signer is trusted by this workspace or organization."
- Add machine-readable evaluator signals for commands, quality gates, policy checks, file/change scope, redaction, confidence, review focus, and final outcome.
- Keep replay privacy-safe by continuing to redact sensitive command output and by adding a redaction/privacy report.
- Document the replay evaluator contract and migration path for legacy sessions.

## Non-Goals

- Do not implement remote transparency logs, Sigstore integration, GitHub OIDC signing, or an organization CA in the first pass.
- Do not change private key storage format unless required for compatibility with signer metadata.
- Do not make `agentreceipt replay` mutate legacy sessions by default.
- Do not expose raw provider logs, raw prompt text, raw command output, or raw `risk_signals` in replay output.
- Do not replace `agentreceipt verify`; this plan improves replay as evaluator-facing evidence while preserving existing verify behavior.

## Assumptions and Open Questions

- Assumption: Existing `model.Verification` signer fields (`signer_public_key`, `signer_key_id`, `signature_algorithm`, `signature`) are the canonical signer metadata for new receipts. Impact: implementation should harden and expose these fields rather than inventing a second signature envelope.
- Assumption: Replay schema version can remain `1` if existing fields stay compatible and new fields are additive. Impact: downstream consumers should not break, but documentation must call out additive fields.
- Assumption: Trust policy should start with local deterministic inputs such as config and CLI-provided trusted key IDs, not a remote trust service. Impact: this supports production loops without adding external service dependencies.
- Assumption: Legacy sessions without embedded signer public keys cannot be made authentic unless the original private key or public key is still available. Impact: replay should report them as integrity-only evidence and optionally support a deliberate migration/re-finalization path.
- Open question: Should unconfigured trust policy default to `signer_trusted=false` or `trust_status=not_configured`? Conservative plan: report `not_configured` separately from `false` so evaluators can decide policy.
- Open question: Should `policy_valid` fail the overall verdict when no trust policy is configured? Conservative plan: use an explicit `overall_verdict` enum instead of overloading a boolean.
- Open question: Should full failed command details be capped in replay JSON or only referenced by artifact path? Conservative plan: include redacted, capped detail fields plus evidence refs to `events.jsonl`.

## Quality Gates

- Setup status: Existing gates are configured in `Makefile` and CI. CI runs `make tools` followed by `make verify` on Ubuntu and macOS.
- Baseline command: `make tools && make verify`
- Format command: `make fmt`
- Format check command: `make fmt-check`
- Lint command: `make lint`
- Test command: `make test`
- Additional gates: `make test-race`, `make security`, `make coverage`, `make build`, `make smoke`, `make verify`
- Per-step full gate sequence: `make fmt && make verify`

## Progress Tracking

- File: `PROGRESS.md`
- Requirement: Create `PROGRESS.md` before any quality-gate setup or implementation work begins.
- Update rule: After each step is completed, update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.

## Changelog Tracking

- File: `CHANGELOG.md`
- Standard: Keep a Changelog 1.0.0, <https://keepachangelog.com/en/1.0.0/>
- Requirement: `CHANGELOG.md` already exists and must be updated before any implementation step is committed.
- Initial content: Keep the existing `# Changelog`, standard preamble, and `## [Unreleased]` section.
- Update rule: After each step is completed and validated, update `CHANGELOG.md` with human-readable notable changes under the appropriate `Unreleased` change-type headings before creating that step's commit.

## Incremental Steps

### Step 0: Progress and Changelog Tracking Setup

Goal: Create durable progress tracking for the evaluator-loop replay work and confirm changelog tracking is ready.

Depends on:
- None

Changes:
- Create `PROGRESS.md` in the project root.
- Add the plan title, source document path, step checklist, current status, and update log.
- Document that `PROGRESS.md` must be updated after every completed step.
- Confirm `CHANGELOG.md` already follows Keep a Changelog 1.0.0 structure.
- Add an `Added` entry under `## [Unreleased]` for establishing evaluator-loop replay implementation tracking.

Acceptance criteria:
- `PROGRESS.md` exists and includes every plan step.
- `PROGRESS.md` records the current status and next step.
- `CHANGELOG.md` retains `# Changelog`, the standard preamble, and `## [Unreleased]`.
- `CHANGELOG.md` has a human-readable `Added` entry for this planning-control milestone.

Validation:
- Run `test -f PROGRESS.md`
- Run `test -f CHANGELOG.md`
- Run `make fmt-check`

Progress:
- Mark Step 0 complete in `PROGRESS.md`, record validation results, set current status to Step 1, and identify Step 1 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for evaluator-loop replay implementation tracking.

Commit:
- `Add evaluator replay progress tracking`

Every implementation step must end with:
1. Run all quality gates: `make fmt && make verify`.
2. Fix any failures before proceeding.
3. Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
4. Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
5. Create a commit for that completed step.

### Step 1: Define Explicit Replay Verification Verdicts

Goal: Make replay verification explain whether a session is internally intact, cryptographically authentic, trusted by policy, and acceptable overall.

Depends on:
- Step 0

Changes:
- Update `internal/replay/replay.go`.
- Add additive fields to `replay.Verification`, preserving existing fields:
  - `integrity_valid`
  - `authenticity_valid`
  - `authenticity_status`
  - `trust_status`
  - `signer_trusted`
  - `policy_valid`
  - `overall_verdict`
  - `overall_reason`
  - `component_results`
- Represent legacy missing embedded signer as:
  - `integrity_valid: true` when event, patch, manifest, and receipt hashes validate.
  - `authenticity_valid: false`.
  - `authenticity_status: "unverifiable"`.
  - `overall_verdict: "integrity_only"` or equivalent documented enum.
- Preserve existing `valid`, `signature_valid`, `signature_error`, `signature_error_code`, and `signed_by` fields for compatibility.
- Update `internal/replay/replay_test.go` with characterization tests for:
  - valid embedded signer receipt.
  - legacy missing embedded signer receipt.
  - tampered event, patch, manifest, and receipt artifacts.
  - signature mismatch with intact hashes.
- Update `cmd/root_test.go` only if replay JSON assertions need to cover the new fields.

Acceptance criteria:
- Replay JSON no longer forces evaluators to infer authenticity from `valid=false`.
- A legacy missing-signer bundle reports integrity separately from authenticity.
- Existing replay consumers can still read the old verification fields.
- Tampering still causes integrity failure.
- Signature mismatch still causes authenticity failure.

Validation:
- Run `make fmt`
- Run `go test ./internal/replay ./cmd`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with Step 1 completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Add a `Changed` entry under `## [Unreleased]` describing explicit replay verification verdicts.

Commit:
- `Split replay verification verdicts`

Every implementation step must end with:
1. Run all quality gates: `make fmt && make verify`.
2. Fix any failures before proceeding.
3. Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
4. Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
5. Create a commit for that completed step.

### Step 2: Add Local Trust Policy Evaluation

Goal: Let production evaluator loops decide whether a cryptographically valid signer is trusted.

Depends on:
- Step 0
- Step 1

Changes:
- Add a small trust-policy module, likely `internal/trust/trust.go` and `internal/trust/trust_test.go`.
- Support trusted signer key IDs as deterministic local inputs.
- Prefer existing config plumbing in `internal/config/config.go`; add fields only if the current config format can support them cleanly.
- Add replay CLI input in `cmd/root.go` if config alone is not sufficient:
  - `agentreceipt replay --trusted-signer-key-id sha256:...`
  - allow repeated values if Cobra flag support is straightforward.
- Update `replay.Options` to carry trust-policy inputs.
- Update `internal/replay/replay.go` so `trust_status`, `signer_trusted`, and `policy_valid` are derived from the signature result and trust policy.
- Add tests in `internal/replay/replay_test.go`, `internal/trust/trust_test.go`, and `cmd/root_test.go` for:
  - matching trusted key ID.
  - untrusted but authentic signer.
  - missing trust policy.
  - malformed trusted key ID input.

Acceptance criteria:
- Replay can report `authenticity_valid=true` while `signer_trusted=false`.
- Replay can report `trust_status=not_configured` without hiding authenticity status.
- Trusted signer decisions are deterministic and do not require network access.
- CLI/config behavior is documented by tests.

Validation:
- Run `make fmt`
- Run `go test ./internal/trust ./internal/replay ./cmd`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with Step 2 completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for local replay signer trust policy evaluation.

Commit:
- `Add replay signer trust policy`

Every implementation step must end with:
1. Run all quality gates: `make fmt && make verify`.
2. Fix any failures before proceeding.
3. Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
4. Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
5. Create a commit for that completed step.

### Step 3: Harden Signer Material and Legacy Migration Semantics

Goal: Ensure new receipts and replay bundles remain portable while legacy sessions are handled explicitly and safely.

Depends on:
- Step 0
- Step 1
- Step 2

Changes:
- Review and harden `internal/receipt/receipt.go`, `internal/model/model.go`, and `internal/signing/signing.go` around signer metadata.
- Ensure `receipt.Finalize` always writes `signer_public_key`, `signer_key_id`, `signature_algorithm`, and `signature`.
- Ensure `receipt.VerifyBundle` and `replay.Build` both use embedded signer material for portable verification when present.
- Add tests in `internal/receipt/receipt_test.go` and `internal/replay/replay_test.go` preventing regressions in embedded signer fields.
- Add a deliberate legacy path:
  - Replay reports missing embedded signer as unauthenticated, not tampered.
  - If re-finalization/migration is added, expose it as an explicit command or documented manual workflow, never as implicit replay mutation.
- If a migration command is added, place CLI tests in `cmd/root_test.go` and keep it opt-in.

Acceptance criteria:
- Newly finalized receipts are portable without local key directories.
- Replay bundles verify signatures using embedded public key material.
- Legacy receipts without embedded signer material are clearly `integrity_only` or equivalent.
- No replay command silently changes old session artifacts.

Validation:
- Run `make fmt`
- Run `go test ./internal/receipt ./internal/replay ./internal/signing ./cmd`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with Step 3 completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Add a `Fixed` or `Changed` entry under `## [Unreleased]` for signer material portability and legacy replay semantics.

Commit:
- `Harden replay signer portability`

Every implementation step must end with:
1. Run all quality gates: `make fmt && make verify`.
2. Fix any failures before proceeding.
3. Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
4. Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
5. Create a commit for that completed step.

### Step 4: Add Evaluator Scoring Signals

Goal: Provide production agents with structured facts they can score without reparsing timeline events.

Depends on:
- Step 0
- Step 1

Changes:
- Update `internal/replay/replay.go`.
- Add an additive top-level `evaluator_signals` object with counts such as:
  - `read_command_count`
  - `write_command_count`
  - `edit_command_count`
  - `test_command_count`
  - `lint_command_count`
  - `typecheck_command_count`
  - `failed_command_count`
  - `network_command_count`
  - `destructive_command_count`
  - `git_mutation_command_count`
  - `dependency_file_change_count`
  - `sensitive_file_change_count`
  - `commit_count`
  - `changed_production_file_count`
  - `changed_test_file_count`
  - `changed_doc_file_count`
- Reuse existing command classification where possible, especially `internal/commandrisk`.
- Add tests in `internal/replay/replay_test.go` covering signal counts for command attempts, command results, file changes, dependency files, sensitive files, and commit-like commands.
- Keep raw command output redacted and capped.

Acceptance criteria:
- Evaluator loops can read one object to understand activity volume and risk-relevant behavior.
- Signals are derived from existing evidence and include evidence refs or are documented as aggregate facts.
- Counts are deterministic for a fixed event log and final patch.

Validation:
- Run `make fmt`
- Run `go test ./internal/replay`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with Step 4 completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for replay evaluator scoring signals.

Commit:
- `Add replay evaluator signals`

Every implementation step must end with:
1. Run all quality gates: `make fmt && make verify`.
2. Fix any failures before proceeding.
3. Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
4. Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
5. Create a commit for that completed step.

### Step 5: Add Quality Gate and Command Failure Evidence Schema

Goal: Make test, lint, typecheck, security, build, smoke, and failed-command evidence directly consumable by reviewer agents.

Depends on:
- Step 0
- Step 4

Changes:
- Update `internal/replay/replay.go`.
- Add a top-level `quality_gates` object with stable gate names:
  - `format`
  - `lint`
  - `tests`
  - `race_tests`
  - `typecheck`
  - `security`
  - `coverage`
  - `build`
  - `smoke`
  - `verify`
- For each gate, include:
  - `status`: `passed`, `failed`, `not_run`, or `unknown`.
  - `commands`.
  - `evidence_refs`.
  - `last_exit_code` when available.
  - `confidence`.
- Add `failed_command_details` or expand `commands` with redacted structured failure fields:
  - `cwd`
  - `time`
  - `exit_code`
  - `failed_reason`
  - `stderr_or_error_summary`
  - `stdout_summary`
  - `output_truncated`
  - `evidence_refs`
- Preserve existing `commands[].output_summary` for compatibility.
- Add tests in `internal/replay/replay_test.go` for successful `make verify`, failed `go test`, missing lint/typecheck gates, and redaction in failure details.

Acceptance criteria:
- Replay output can answer "what gates ran and did they pass?" without natural-language parsing.
- Failed command details are specific enough for evaluators to identify the failure.
- Sensitive output remains redacted.
- Existing summary booleans remain populated.

Validation:
- Run `make fmt`
- Run `go test ./internal/replay`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with Step 5 completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for replay quality-gate and failed-command evidence.

Commit:
- `Add replay quality gate evidence`

Every implementation step must end with:
1. Run all quality gates: `make fmt && make verify`.
2. Fix any failures before proceeding.
3. Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
4. Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
5. Create a commit for that completed step.

### Step 6: Add Patch Semantic Summary

Goal: Summarize the final patch in a machine-readable way so evaluator agents can focus review without reading the full diff first.

Depends on:
- Step 0
- Step 4

Changes:
- Add `internal/replay/patch_summary.go` and `internal/replay/patch_summary_test.go`, or keep the parser in `internal/replay/replay.go` if it remains small.
- Parse `diffs/final.patch` into a top-level `patch_summary` object with:
  - file counts by category: production, test, docs, config, dependency, generated or unknown.
  - additions and deletions when available from diff hunks.
  - changed file entries with path, action, category, sensitive/dependency flags, and evidence refs.
  - likely changed Go symbols where simple and reliable from hunk context or Go parser fallback.
  - test coverage relationship signals such as `tests_changed` and `production_changed_without_tests_changed`.
- Avoid including full hunk bodies or sensitive diff content.
- Add tests in `internal/replay/patch_summary_test.go` for Go code, tests, docs, dependency/config files, binary/renamed files, and malformed patches.

Acceptance criteria:
- Replay output identifies what changed at a useful semantic level.
- Patch summary does not leak raw diff content.
- Malformed or missing final patch produces a gap, not a panic.
- The existing `files` array remains available.

Validation:
- Run `make fmt`
- Run `go test ./internal/replay`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with Step 6 completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for replay patch semantic summaries.

Commit:
- `Add replay patch summary`

Every implementation step must end with:
1. Run all quality gates: `make fmt && make verify`.
2. Fix any failures before proceeding.
3. Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
4. Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
5. Create a commit for that completed step.

### Step 7: Add Policy Checks and Review Focus

Goal: Turn replay evidence into evaluator-ready checks and review prompts without making unsupported policy decisions.

Depends on:
- Step 0
- Step 4
- Step 5
- Step 6

Changes:
- Update `internal/replay/replay.go`.
- Add a top-level `policy_checks` array with deterministic checks such as:
  - `target_file_read_before_edit`
  - `related_context_read_before_edit`
  - `tests_run_after_code_changes`
  - `lint_run_after_code_changes`
  - `typecheck_run_when_applicable`
  - `destructive_command_used`
  - `network_command_used`
  - `dependency_file_changed`
  - `sensitive_file_changed`
  - `ci_or_security_file_changed`
  - `generated_file_changed`
  - `commit_created`
- Each policy check should include:
  - `status`: `pass`, `fail`, `warn`, `not_applicable`, or `unknown`.
  - `message`.
  - `confidence`.
  - `evidence_refs`.
- Add a top-level `review_focus` array generated from verification gaps, quality gates, patch summary, sensitive paths, dependency changes, policy warnings, and failed commands.
- Keep focus prompts factual and bounded; avoid scoring recommendations that are not supported by evidence.
- Add tests in `internal/replay/replay_test.go` for policy statuses and review focus generation.

Acceptance criteria:
- Evaluator agents can consume policy checks directly.
- Review prompts are specific to the session and include evidence refs where possible.
- Checks distinguish `unknown` from `fail` when evidence is insufficient.
- Replay remains factual and does not expose raw provider risk fields.

Validation:
- Run `make fmt`
- Run `go test ./internal/replay`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with Step 7 completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for replay policy checks and reviewer focus prompts.

Commit:
- `Add replay policy checks`

Every implementation step must end with:
1. Run all quality gates: `make fmt && make verify`.
2. Fix any failures before proceeding.
3. Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
4. Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
5. Create a commit for that completed step.

### Step 8: Add Privacy Report, Claim Confidence, and Outcome Classification

Goal: Make replay safe and actionable for automated production loops by exposing privacy handling, confidence per claim, and a final outcome state.

Depends on:
- Step 0
- Step 1
- Step 4
- Step 5
- Step 7

Changes:
- Update `internal/replay/replay.go`.
- Add a top-level `privacy` or `redaction` report:
  - `redaction_applied`
  - `redacted_fields`
  - `redaction_patterns`
  - `output_caps`
  - `sensitive_content_detected`
  - `raw_provider_logs_exposed`
- Add `claims` or per-section claim metadata for key evaluator facts:
  - verification verdict.
  - signer authenticity.
  - signer trust.
  - quality gate statuses.
  - policy check statuses.
  - outcome classification.
- Each claim should include confidence and evidence refs.
- Add a top-level `outcome` object:
  - `status`: `completed`, `completed_with_gaps`, `failed`, `abandoned`, `committed`, or `needs_human_review`.
  - `reasons`.
  - `confidence`.
  - `evidence_refs`.
- Add tests in `internal/replay/replay_test.go` for:
  - privacy report with redacted secret-like output.
  - no raw provider logs exposed.
  - completed session with passing gates.
  - completed session with gaps.
  - failed or abandoned session inference from non-finalized state or failed gates.

Acceptance criteria:
- Replay explicitly reports what was redacted and where raw evidence is not exposed.
- Evaluator agents can determine final outcome without interpreting free-text gaps.
- Confidence and evidence refs are attached to key claims.
- Existing privacy tests continue to pass.

Validation:
- Run `make fmt`
- Run `go test ./internal/replay`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with Step 8 completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for replay privacy reporting, claim confidence, and outcome classification.

Commit:
- `Add replay evaluator outcome reporting`

Every implementation step must end with:
1. Run all quality gates: `make fmt && make verify`.
2. Fix any failures before proceeding.
3. Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
4. Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
5. Create a commit for that completed step.

### Step 9: Document the Production Replay Evaluator Contract

Goal: Make the new replay output contract understandable and safe for humans and downstream agent loops.

Depends on:
- Step 0
- Step 1
- Step 2
- Step 3
- Step 4
- Step 5
- Step 6
- Step 7
- Step 8

Changes:
- Update `README.md` replay documentation.
- Add or update a focused document such as `docs/replay-evaluator-contract.md` if a docs directory exists or if README would become too large.
- Document:
  - integrity vs authenticity vs trust vs policy.
  - legacy missing signer behavior.
  - trust policy inputs.
  - quality gate schema.
  - evaluator signals.
  - policy checks.
  - privacy/redaction guarantees.
  - outcome statuses.
  - stable additive schema compatibility rules.
- Update `evaluator-loop-spec.md.` only if the team wants the source spec to reflect final decisions; otherwise leave it as historical input.
- Add README or CLI smoke assertions in `cmd/root_test.go` only if existing tests cover documentation-facing examples.

Acceptance criteria:
- A production evaluator author can consume replay JSON without reading source code.
- Documentation explains why `signature_valid=false` may coexist with valid hashes.
- Documentation explains how to configure signer trust and how to handle legacy receipts.
- Documentation states privacy boundaries and non-goals.

Validation:
- Run `make fmt`
- Run `make verify`

Progress:
- Update `PROGRESS.md` with Step 9 completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Add a `Changed` entry under `## [Unreleased]` for replay evaluator contract documentation.

Commit:
- `Document replay evaluator contract`

Every implementation step must end with:
1. Run all quality gates: `make fmt && make verify`.
2. Fix any failures before proceeding.
3. Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
4. Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
5. Create a commit for that completed step.
