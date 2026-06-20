# Implementation Plan

## Source Documents
- Path: `/Users/alexmetelli/source/agentreceipt/docs/REPLAY_SPECS.md`
  - Role: Primary replay product contract and acceptance criteria.
  - Summary: Add verifier-facing `agentreceipt replay --session <ar_ses_...> --json` output and optional `--bundle <dir>` export. Replay must reconstruct one session from existing local artifacts (`events.jsonl`, `receipt.json`, `manifest.json`, `diffs/final.patch`, and optional provider traces), must not rerun commands or call models, must emit explicit verification status, gaps, risks, verifier tasks, stable evidence references, and must tolerate incomplete evidence without crashing.

## Goals
- Add a replay command that produces one structured JSON object with `schema_version`, `kind`, `session_id`, `generated_at`, `source`, `verification`, `summary`, `timeline`, `commands`, `files`, `risks`, `gaps`, `verifier_tasks`, and `artifacts`.
- Require `--session` for replay and avoid implicit active/latest session behavior.
- Keep replay artifact-only: no shell command reruns, patch application, model calls, scoring, or workspace mutation.
- Reuse existing event-log replay, receipt verification, provider evidence, command risk, and review-summary logic through replay-safe helpers.
- Support portable replay bundles containing `replay.json`, receipt, manifest, event log, final patch, and optional redacted provider traces.
- Preserve local-first privacy defaults: no raw prompts, no raw tool output, no raw provider logs by default, and secret redaction before replay output.
- Ensure tampered `events.jsonl`, `receipt.json`, `manifest.json`, or `diffs/final.patch` marks replay invalid with concrete gaps and verifier tasks.

## Non-Goals
- Do not implement policy enforcement, merge blocking, sandboxing, agent control, model scoring, or verdict persistence beyond replay output.
- Do not add implicit latest-session replay, because verifier automation needs an explicit session ID.
- Do not add Markdown or terminal replay formats in this rollout; JSON is the verifier contract.
- Do not include raw Codex or Claude provider logs in bundles; only normalized/redacted trace exports are eligible.
- Do not redesign the existing receipt, review, capture, or session storage model except for narrowly scoped helper extraction needed by replay.
- Do not fetch or embed the external OKF source documents cited by the spec; treat the supplied replay spec as the authoritative requirement source for this plan.

## Assumptions and Open Questions
- Assumption: `--json` should be accepted for the documented command contract, but JSON is also the default replay output. Impact: existing scripts can call either `agentreceipt replay --session <id>` or `agentreceipt replay --session <id> --json` and receive one JSON object.
- Assumption: When `--bundle <dir>` is supplied, the command writes the same replay object to `<dir>/replay.json` and still writes that JSON object to stdout, with no human status text mixed into stdout. Impact: verifier automation can pipe stdout while also collecting a portable bundle.
- Assumption: Replay should primarily target finalized sessions, but should emit a gap rather than crash if the session state is active, finalizing, or otherwise incomplete. Impact: `source.session_state` and `gaps` explain why confidence or verification is limited.
- Assumption: Artifact-only verification should validate the signed receipt against stored artifacts and embedded signer material without checking the current workspace diff. Impact: replay remains compliant with the "must not rerun shell commands" requirement even though current local `receipt.Verify` also checks the live workspace diff.
- Assumption: Existing normalized provider traces under `provider/codex/traces/` are treated as redacted enrichment, while `provider/codex/imported-session.jsonl` and other raw provider logs are excluded from replay bundles. Impact: bundle contents stay privacy-preserving by default.
- Open question: Should future replay support a quiet bundle-only mode that suppresses stdout? Impact if unresolved: the initial implementation favors the verifier JSON contract and can add a separate flag later if users need silent bundle creation.

## Quality Gates
- Setup status: Existing gates are configured in `Makefile`, `.golangci.yml`, `.github/workflows/ci.yml`, and `.github/workflows/release.yml`; no separate Quality Gates Setup step is required. If local tool binaries are missing, run `make tools` once before the baseline gate.
- Baseline command: `make verify`
- Format command: `make fmt` followed by `make fmt-check`
- Lint command: `make lint`
- Test command: `make test`
- Additional gates: `make test-race`, `make security`, `make coverage`, `make build`, `make smoke`, and aggregate CI parity with `make verify`

## Progress Tracking
- File: `PROGRESS.md`
- Requirement: Create `PROGRESS.md` before any quality-gate setup or implementation work begins.
- Update rule: After each step is completed, update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.

## Changelog Tracking
- File: `CHANGELOG.md`
- Standard: Keep a Changelog 1.0.0, <https://keepachangelog.com/en/1.0.0/>
- Requirement: Ensure `CHANGELOG.md` exists before any quality-gate setup or implementation work begins. The repository already has this file, so preserve existing history and verify the required structure instead of recreating it.
- Initial content: Maintain `# Changelog`, the standard preamble, and an `## [Unreleased]` section.
- Update rule: After each step is completed and validated, update `CHANGELOG.md` with human-readable notable changes under the appropriate `Unreleased` change-type headings before creating that step's commit. Keep newest entries first and use `Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, and `Security` headings as applicable.

## Incremental Steps

### Step 0: Progress and Changelog Tracking Setup
Goal: Create durable progress tracking and verify changelog tracking before any implementation work starts.

Depends on:
- None

Changes:
- Create `PROGRESS.md` in the project root.
- Add the plan title, source document path, a checklist for all steps in this plan, current status, and a short update log.
- Document that `PROGRESS.md` must be updated after every completed step with validation results, commit reference if available, current status, and next step.
- Preserve the existing `CHANGELOG.md`.
- Verify `CHANGELOG.md` follows Keep a Changelog 1.0.0 structure with `# Changelog`, the standard preamble, and `## [Unreleased]`.
- Document that `CHANGELOG.md` must be updated after each step is completed and validated, before that step is committed.
- Run the baseline gate `make verify` before starting Step 1. If it fails because local tools are missing, run `make tools`, rerun `make verify`, and record both results.

Acceptance Criteria:
- `PROGRESS.md` exists and includes the step checklist and update rule.
- `CHANGELOG.md` exists, retains existing release history, and has a valid `## [Unreleased]` section.
- Baseline quality-gate result is recorded in `PROGRESS.md`.

Validation:
- Confirm `PROGRESS.md` exists and contains the step checklist.
- Confirm `CHANGELOG.md` exists and follows the required Keep a Changelog 1.0.0 structure.
- Run `make verify` as the baseline gate and record the result.

Progress:
- Mark Step 0 complete in `PROGRESS.md`, record validation results, set current status to Step 1, and identify the next step.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for establishing replay implementation progress tracking if `PROGRESS.md` is newly created.

Commit:
- `docs: add replay implementation tracking`

### Step 1: Extract Replay-Safe Evidence Analysis
Goal: Make existing review evidence summarization reusable without invoking git commands or terminal renderers.

Depends on:
- Step 0

Changes:
- Add a focused pure-evidence package or file, such as `internal/evidence/evidence.go`, for replay-safe analysis of in-memory `[]model.Event`.
- Move or wrap the current pure portions of `internal/review/review.go`: summary extraction, capture confidence, risk calculation, gaps, focus prompts, timeline construction, command status pairing, changed-file sorting, and configured quality-command detection.
- Keep git-specific review logic, branch summaries, terminal rendering, Markdown rendering, and command execution inside `internal/review`.
- Update `review.Build` to call the extracted evidence helpers while preserving current review JSON and terminal behavior.
- Enhance provider evidence accessors if needed so command-result replay can read `exit_code`, `stdout_truncated`, `failed_reason`, and stderr/error metadata without exposing raw output.
- Add or update tests in `internal/evidence`, `internal/review`, and `internal/providerevidence` for paired command attempts/results, unpaired results, test/lint/typecheck detection, sensitive/dependency file flags, provider risk signals, and stable ordering.

Acceptance Criteria:
- `review.Build` behavior is unchanged for existing tests.
- The new evidence helper does not import `os/exec` or run git commands.
- Evidence helper output is deterministic for the same event list and config.
- Command result helpers expose status, exit code, truncation, and failure metadata needed by replay while keeping raw stdout optional and redacted.

Validation:
- Run `make fmt`.
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

End-of-step:
- Fix any quality-gate failures before proceeding.
- Update `PROGRESS.md` with Step 1 completion notes, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` under `## [Unreleased]` with a notable `Changed` entry for extracting replay-safe evidence analysis after validation and before committing.
- Create the commit.

Commit:
- `refactor: extract replay-safe evidence analysis`

### Step 2: Add Artifact-Only Receipt Verification
Goal: Reuse receipt verification logic for replay without checking the live workspace or running git commands.

Depends on:
- Step 0
- Step 1

Changes:
- Refactor `internal/receipt/receipt.go` to expose a shared artifact verification helper, for example `VerifyArtifacts(options)` or `VerifyLayout(layout, options)`, that validates:
  - event chain hash from `events.jsonl`
  - manifest hash from `manifest.json`
  - final patch hash from `diffs/final.patch`
  - unsigned receipt hash from `receipt.json`
  - receipt signature using embedded signer material where possible
  - unknown top-level receipt fields
- Keep current `receipt.Verify` behavior for local verification, including the existing current-workspace diff check.
- Update `receipt.VerifyBundle` to use the same artifact-only helper.
- Ensure replay can receive verification details and warnings without causing command execution.
- Add tests in `internal/receipt/receipt_test.go` for valid artifacts, tampered events, tampered manifest, tampered final patch, tampered receipt, unknown receipt fields, missing signature material, and embedded-key verification.

Acceptance Criteria:
- `receipt.Verify` and `receipt.VerifyBundle` preserve existing behavior.
- The new artifact verification path detects tampering in each required artifact.
- The artifact verification path does not execute git or shell commands.
- Verification results include booleans and warnings granular enough for replay JSON fields and gaps.

Validation:
- Run `make fmt`.
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

End-of-step:
- Fix any quality-gate failures before proceeding.
- Update `PROGRESS.md` with Step 2 completion notes, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` under `## [Unreleased]` with a notable `Changed` or `Fixed` entry for artifact-only receipt verification after validation and before committing.
- Create the commit.

Commit:
- `refactor: add artifact-only receipt verification`

### Step 3: Build Replay JSON Model and Session Builder
Goal: Produce the verifier-facing replay object from stored session artifacts.

Depends on:
- Step 0
- Step 1
- Step 2

Changes:
- Add `internal/replay` with typed JSON structs for:
  - top-level replay report
  - source metadata
  - verification details
  - summary
  - timeline items
  - command records
  - file records
  - risk records
  - gaps
  - verifier tasks
  - artifact references
- Implement a builder such as `replay.Build(ctx, replay.Options{RepoPath, SessionID, GeneratedAt})`.
- Require `SessionID` in replay options; do not support `Last` or active/latest fallback.
- Resolve storage through `storage.NewLayout(repoRoot, sessionID)`.
- Read `state.json`, `manifest.json`, `receipt.json`, `events.jsonl`, and `diffs/final.patch` directly from the session layout.
- Use `eventlog.ReadFile` and `eventlog.Replay` for timeline and event-chain integrity.
- Use the artifact-only receipt verification helper from Step 2 for replay `verification`.
- Use the replay-safe evidence helpers from Step 1 for summary, command detection, risk, gaps, and focus.
- Add stable evidence references, for example `events.jsonl#seq=<n>`, `receipt.json`, `manifest.json`, `diffs/final.patch`, and provider trace paths when present.
- Pair command attempts and results by `call_id` first and command text second where possible. Emit unpaired attempts/results as commands with explicit `unknown` status or gaps.
- Populate command records with command text, kind, status, exit code, risk signals, output summary, `stdout_truncated`, confidence, and evidence references without raw stdout by default.
- Populate file records with repo-relative path, action, sensitive/dependency flags, and whether the path appears in `diffs/final.patch` by parsing patch headers instead of running git.
- Populate gaps for missing provider evidence, unknown command status, unpaired command result, missing tests/lint/typecheck, invalid/tampered artifacts, missing optional artifacts, and non-finalized session state.
- Populate verifier tasks from concrete risks and gaps, such as inspecting failed commands, reviewing sensitive paths, confirming tests for code changes, or investigating invalid verification.
- Apply secret redaction to all replay output fields that may contain command or provider text, even if the stored provider event was captured with relaxed privacy settings.
- Add `internal/replay/replay_test.go` with fixture sessions covering:
  - valid finalized session
  - missing provider evidence
  - failed command with exit code
  - unpaired command result
  - sensitive/dependency file changes
  - missing tests/lint/typecheck gaps
  - non-finalized session gap
  - tampered artifact invalid replay
  - stable ordering and stable evidence references
  - no raw stdout or prompt leakage

Acceptance Criteria:
- `replay.Build` returns the top-level JSON shape from the spec.
- A verifier can consume replay JSON without parsing Markdown.
- The same finalized session produces stable ordering and stable evidence references.
- Tampered required artifacts set `verification.valid` to false and add gaps/tasks.
- Missing provider evidence creates a gap and does not crash.
- Each risk or focus item links back to concrete evidence where possible.
- Replay builder does not run shell commands, apply patches, call models, or mutate workspace files.

Validation:
- Run `make fmt`.
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

End-of-step:
- Fix any quality-gate failures before proceeding.
- Update `PROGRESS.md` with Step 3 completion notes, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` under `## [Unreleased]` with a notable `Added` entry for the replay JSON builder after validation and before committing.
- Create the commit.

Commit:
- `feat: build verifier replay json`

### Step 4: Add the `agentreceipt replay` CLI Command
Goal: Expose replay JSON through the documented command surface.

Depends on:
- Step 0
- Step 1
- Step 2
- Step 3

Changes:
- Add `newReplayCommand()` in `cmd/root.go` or a new focused `cmd/replay.go` file following existing Cobra command patterns.
- Register the replay command in `NewRootCommand`.
- Add flags:
  - `--session <id>` required
  - `--json` accepted for the product contract, with JSON as the default format
- Do not register `--bundle` until Step 5 unless bundle writing is implemented in the same commit.
- Validate `--session` with existing storage session ID validation.
- Return a clear error when `--session` is omitted.
- Encode replay JSON deterministically using typed structs and stable slices.
- Ensure no terminal color, Markdown, or human status text is mixed into JSON stdout.
- Update `cmd/root_test.go` command discovery, help, flag registration, missing-session behavior, and JSON output tests.
- Update `rootLong` and command help text to mention `replay`.

Acceptance Criteria:
- `agentreceipt replay --session <id> --json` emits one JSON object with `kind: "agentreceipt.session_replay"`.
- `agentreceipt replay --session <id>` emits the same JSON object.
- `agentreceipt replay` fails with a clear missing-session error.
- The replay command appears in root help and command discovery tests.
- CLI tests prove the command does not use implicit latest-session behavior.

Validation:
- Run `make fmt`.
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

End-of-step:
- Fix any quality-gate failures before proceeding.
- Update `PROGRESS.md` with Step 4 completion notes, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` under `## [Unreleased]` with a notable `Added` entry for the replay CLI command after validation and before committing.
- Create the commit.

Commit:
- `feat: add replay command`

### Step 5: Implement Portable Replay Bundles
Goal: Write a portable verifier bundle with replay JSON and source artifacts.

Depends on:
- Step 0
- Step 1
- Step 2
- Step 3
- Step 4

Changes:
- Add `replay.WriteBundle(ctx, options, bundleDir)` or equivalent in `internal/replay`.
- Create the bundle directory structure:
  - `replay.json`
  - `receipt.json`
  - `manifest.json`
  - `events.jsonl`
  - `diffs/final.patch`
  - optional `provider/codex/traces/` redacted normalized traces
- Copy only the artifacts allowed by the spec.
- Exclude raw provider logs such as `provider/codex/imported-session.jsonl`.
- Preserve stable relative paths in replay artifact references.
- Hash every copied artifact and include those hashes in the replay `artifacts` array.
- Decide overwrite behavior conservatively: create missing directories and overwrite known bundle files atomically, but do not delete unrelated user files in the target directory.
- Wire `agentreceipt replay --session <id> --json --bundle <dir>` to write the bundle and emit the replay JSON object to stdout.
- Add tests for bundle layout, replay.json content, copied artifact hashes, optional traces, missing optional provider traces, and raw log exclusion.

Acceptance Criteria:
- The bundle layout matches the product contract.
- `replay.json` is byte-for-byte the replay object emitted by the command except for expected JSON formatting if the implementation intentionally formats file and stdout differently.
- Required artifacts are copied and hashed.
- Missing provider traces do not fail bundle creation; they create a gap when provider evidence is missing.
- Raw prompts, raw tool output, and raw provider logs are not copied by default.

Validation:
- Run `make fmt`.
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

End-of-step:
- Fix any quality-gate failures before proceeding.
- Update `PROGRESS.md` with Step 5 completion notes, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` under `## [Unreleased]` with a notable `Added` entry for portable replay bundles after validation and before committing.
- Create the commit.

Commit:
- `feat: write portable replay bundles`

### Step 6: Add End-to-End Replay Coverage and Smoke Checks
Goal: Prove replay works through the built binary and the normal session lifecycle.

Depends on:
- Step 0
- Step 1
- Step 2
- Step 3
- Step 4
- Step 5

Changes:
- Extend `scripts/smoke.sh` to run replay against the smoke session:
  - `agentreceipt --repo "$repo" replay --session "$session_id" --json`
  - `agentreceipt --repo "$repo" replay --session "$session_id" --json --bundle "$tmpdir/replay"`
- Assert replay stdout contains `"kind": "agentreceipt.session_replay"`, the smoke `session_id`, `"valid": true`, command evidence, artifact references, and no raw provider log text.
- Assert the bundle contains `replay.json`, `receipt.json`, `manifest.json`, `events.jsonl`, and `diffs/final.patch`.
- Add a smoke assertion that `replay` without `--session` fails.
- Add focused CLI integration tests if smoke coverage alone cannot assert error behavior cleanly.

Acceptance Criteria:
- `make smoke` exercises the replay JSON and bundle paths.
- Smoke coverage proves replay requires explicit `--session`.
- Smoke coverage proves replay output stays machine-readable and artifact-backed.

Validation:
- Run `make fmt`.
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

End-of-step:
- Fix any quality-gate failures before proceeding.
- Update `PROGRESS.md` with Step 6 completion notes, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` under `## [Unreleased]` with a notable `Added` or `Fixed` entry for replay smoke coverage after validation and before committing.
- Create the commit.

Commit:
- `test: add replay smoke coverage`

### Step 7: Document Replay Usage and Contracts
Goal: Document the replay command, JSON contract, privacy behavior, and bundle layout for users and future maintainers.

Depends on:
- Step 0
- Step 1
- Step 2
- Step 3
- Step 4
- Step 5
- Step 6

Changes:
- Update `README.md`:
  - Add replay to the core workflow after verify/export or near verifier workflows.
  - Add quick command reference entries for `agentreceipt replay --session <id> --json` and `agentreceipt replay --session <id> --json --bundle ./replay`.
  - Explain that replay requires explicit `--session` and does not rerun commands.
  - Document the bundle layout and privacy defaults at a user level.
- Update `docs/PRD.md` current-state product requirements after implementation:
  - Add replay to implemented scope, core workflow, command surface, evidence model, verification model, privacy, limitations, and build/test sections.
- Update `docs/TECH_SPEC.md` after implementation:
  - Add replay command behavior, replay schema, artifact-only verification, bundle writing, and test coverage notes.
- Keep docs factual and aligned with implemented behavior.

Acceptance Criteria:
- README command references match actual CLI flags.
- PRD and TECH_SPEC describe implemented replay behavior, not aspirational behavior.
- Docs explicitly state that replay does not rerun commands, reapply patches, call models, or include raw provider logs by default.
- Docs explain that missing evidence appears as gaps.

Validation:
- Run `make fmt`.
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

End-of-step:
- Fix any quality-gate failures before proceeding.
- Update `PROGRESS.md` with Step 7 completion notes, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` under `## [Unreleased]` with a notable `Added` or `Changed` entry for replay documentation after validation and before committing.
- Create the commit.

Commit:
- `docs: document replay command`

### Step 8: Final Replay Acceptance Audit
Goal: Verify the implemented feature satisfies every replay acceptance criterion and leaves the repository ready for review.

Depends on:
- Step 0
- Step 1
- Step 2
- Step 3
- Step 4
- Step 5
- Step 6
- Step 7

Changes:
- Review `docs/REPLAY_SPECS.md` acceptance criteria against implemented code and tests.
- Add any missing regression tests discovered during the audit.
- Confirm no replay path invokes `exec.Command`, git, patch application, model calls, or workspace mutation.
- Confirm no replay bundle includes raw provider logs or prompt files.
- Confirm tamper cases for `events.jsonl`, `receipt.json`, `manifest.json`, and `diffs/final.patch` are covered.
- Confirm `PROGRESS.md` and `CHANGELOG.md` are current before the final commit.

Acceptance Criteria:
- Every acceptance criterion from `docs/REPLAY_SPECS.md` is either implemented and tested or explicitly recorded as an open follow-up with rationale.
- `make verify` passes from a clean working tree after all replay changes.
- Final `PROGRESS.md` status identifies the replay plan as complete.
- `CHANGELOG.md` has human-readable `Unreleased` entries for the replay feature, tests, and docs.

Validation:
- Run `make fmt`.
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

End-of-step:
- Fix any quality-gate failures before proceeding.
- Update `PROGRESS.md` with Step 8 completion notes, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` under `## [Unreleased]` with any final notable replay fixes or audit notes after validation and before committing.
- Create the commit.

Commit:
- `test: audit replay acceptance criteria`
