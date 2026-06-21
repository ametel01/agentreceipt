# Implementation Plan

## Source Documents

- Path: `/Users/alexmetelli/source/agentreceipt/improve.md`
  - Role: Primary product-direction note.
  - Summary: Reposition AgentReceipt around coding-agent CLI review loops by adding a compact machine-readable focus surface, ranked review tasks, per-file evidence dossiers, instruction capture, start-state separation, stable schemas, deterministic exit codes, local diff equivalence checks, loop-health signals, and resolvable evidence references. The note explicitly rejects agent rankings, generalized trust scores, and orchestration features.
- Path: `docs/replay-evaluator-contract.md`
  - Role: Existing replay contract and compatibility constraint.
  - Summary: `agentreceipt replay` already provides verifier-facing factual evidence with `verification`, `quality_gates`, `policy_checks`, `review_focus`, `claims`, `outcome`, commands, files, gaps, and artifacts. New reviewer-loop features must be additive, deterministic, privacy-preserving, and must not turn replay into a scoring or policy-enforcement system.
- Path: `docs/GITHUB_PR_WORKFLOW_DESIGN.md`
  - Role: Supporting design constraint for PR and CI workflows.
  - Summary: Receipt artifacts are local-first and deterministic. CI-assisted and PR workflows must verify artifact integrity and diff parity without uploading raw prompts, raw provider logs, transcripts, private keys, or unredacted tool output by default.
- Path: `README.md`
  - Role: User-facing workflow and command-surface context.
  - Summary: The CLI currently centers on `start`, `stop`, `review`, `verify`, `replay`, `export`, and `pr comment`. Watch and review output are human-readable, while `replay --json` is the existing machine-readable evaluator surface.

## Goals

- Add a compact reviewer-agent focus contract that lets coding-agent loops quickly decide whether a session can pass, needs review, must block, or is unverifiable.
- Add ranked, structured review tasks that point reviewer agents at the exact files, gates, commands, and evidence references worth inspecting.
- Add per-file evidence dossiers that map changed files to reads, edits, tests, commands, policy checks, risks, and review reasons.
- Capture coding-agent instruction files at session start so replay and focus reports can describe which local rules were in force.
- Separate pre-existing workspace state from agent-introduced changes so reviewers do not blame unrelated dirty files on the session.
- Provide stable JSON schema output and machine-oriented exit codes for automation.
- Add first-class local diff equivalence checks so loops can verify that the receipt final patch matches the branch, workspace, bundle, or supplied PR patch under review.
- Add loop-health signals and evidence reference dereferencing to reduce brittle manual parsing of `events.jsonl`.
- Keep all new behavior local-first, deterministic, privacy-preserving, and additive to existing replay contracts.

## Non-Goals

- Do not build an agent orchestrator, model router, sandbox, permission layer, or managed execution wrapper.
- Do not introduce agent, model, vendor, developer, or session trust rankings.
- Do not replace human or reviewer-agent judgment with an opaque score.
- Do not upload raw prompts, raw provider logs, transcripts, raw tool output, private keys, or local blob contents by default.
- Do not make GitHub App enforcement, hosted dashboards, or team policy distribution part of this implementation.
- Do not break existing `review`, `replay`, `verify`, `export`, or PR Markdown behavior.
- Do not remove or rename existing replay fields; schema evolution must be additive.

## Assumptions and Open Questions

- Assumption: The first implementation should make `agentreceipt focus --json` the primary machine loop surface, backed by existing replay construction. Impact: this avoids duplicating receipt parsing and keeps focus behavior aligned with replay evidence.
- Assumption: `agentreceipt focus` should require either `--session <id>` or `--replay <path>`; it should not implicitly pick the latest session. Impact: deterministic reviewer loops avoid accidentally reviewing the wrong session.
- Assumption: Instruction capture should initially cover repository-local `AGENTS.md` and `CLAUDE.md` files discovered from the repository root and relevant parent directories. Impact: broader provider-specific instruction files can be added later without blocking the first useful rule-capture path.
- Assumption: Instruction capture should store path, hash, size, mtime, and bounded deterministic rule summaries, not raw prompt/session text. Impact: review evidence remains useful while avoiding unnecessary privacy expansion.
- Assumption: Machine exit codes should apply first to `focus` and `verify diff`, not every existing command. Impact: the loop-facing commands get deterministic control flow without changing long-standing command behavior unexpectedly.
- Assumption: Stable JSON schemas can be hand-authored and embedded or read from a package directory. Impact: implementation can avoid adding a schema-generation dependency unless maintainers explicitly want one.
- Open question: Should `agentreceipt focus` support human text output in the first release, or should it be JSON-only until the contract stabilizes? Conservative default: implement `--json` first and keep terminal text minimal.
- Open question: Should `agentreceipt verify diff --bundle` read a full replay bundle or the smaller receipt bundle accepted by `verify bundle`? Conservative default: accept the artifact bundle shape that contains `receipt.json`, `manifest.json`, `events.jsonl`, and `diffs/final.patch`, then optionally use `replay.json` when present.
- Open question: Should `validation_after_last_edit` treat `make verify` as satisfying all gates or preserve individual gate statuses separately? Conservative default: preserve individual gate statuses and additionally mark `verify` as a high-confidence aggregate gate.

## Quality Gates

- Setup status: Existing gates are configured in `Makefile`, `.golangci.yml`, and GitHub Actions. CI installs tools with `make tools` and runs `make verify` on Ubuntu and macOS.
- Baseline command: `make tools && make verify`
- Format command: `make fmt-check`
- Lint command: `make lint`
- Test command: `make test`
- Additional gates: `make test-race`, `make security`, `make coverage`, `make build`, `make smoke`
- Full gate command: `make verify`
- Formatting repair command: `make fmt`
- Notes: `make verify` writes expected local artifacts such as `coverage.out` and `./agentreceipt` because it includes coverage and build targets. Executors should treat those as expected gate artifacts, not feature output.

## Progress Tracking

- File: `PROGRESS.md`
- Requirement: Create `PROGRESS.md` before any quality-gate setup or implementation work begins.
- Update rule: After each step is completed, update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.

## Changelog Tracking

- File: `CHANGELOG.md`
- Standard: Keep a Changelog 1.0.0, <https://keepachangelog.com/en/1.0.0/>
- Requirement: `CHANGELOG.md` already exists and follows the expected structure. Preserve existing release notes. If absent in a future branch, create it before any quality-gate setup or implementation work begins.
- Initial content: Ensure the file includes `# Changelog`, the standard preamble, and an `## [Unreleased]` section.
- Update rule: After each step is completed and validated, update `CHANGELOG.md` with human-readable notable changes under the appropriate `Unreleased` change-type headings before creating that step's commit.

## Incremental Steps

### Step 0: Progress and Changelog Tracking Setup

Goal: Create durable progress tracking and confirm changelog tracking before implementation begins.

Depends on:
- None

Changes:
- Create `PROGRESS.md` in the project root.
- Add the plan title, source documents, step checklist, current status, and a short update log.
- Document that `PROGRESS.md` must be updated after every completed step.
- Confirm `CHANGELOG.md` exists in the project root and preserves the Keep a Changelog 1.0.0 structure.
- If `CHANGELOG.md` is missing on the executor branch, create it with `# Changelog`, the standard preamble, and `## [Unreleased]`.
- Do not start feature implementation in this step.

Acceptance Criteria:
- `PROGRESS.md` exists and contains every step from this plan.
- `PROGRESS.md` identifies Step 1 as the next implementation step.
- `CHANGELOG.md` exists and contains `# Changelog` plus `## [Unreleased]`.
- Existing `CHANGELOG.md` release sections are preserved.

Validation:
- Run `test -f PROGRESS.md`
- Run `grep -q "Step 1: Add the compact focus report model" PROGRESS.md`
- Run `test -f CHANGELOG.md`
- Run `grep -q "^## \\[Unreleased\\]" CHANGELOG.md`

Progress:
- Mark Step 0 complete in `PROGRESS.md`, record validation results, set the current status, and identify Step 1 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for establishing progress tracking for the coding-agent reviewer-loop plan.

Commit:
- `docs: add reviewer loop implementation tracking`

Required End-of-Step Actions:
- Run all listed validation commands for this step.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with the notable completed work under `## [Unreleased]`.
- Create a commit for this completed step.

### Step 1: Add the compact focus report model

Goal: Add a deterministic internal model for the compact reviewer-agent focus surface without exposing it through the CLI yet.

Depends on:
- Step 0

Changes:
- Add a new focus report type in `internal/replay`, for example `FocusReport`, `FocusVerdict`, `FocusReason`, `ReviewTask`, `FocusChangedFile`, and `FailedGate`.
- Set `kind` to `agentreceipt.session_focus` and keep `schema_version` aligned with existing schema version conventions.
- Define the first verdict enum as `pass`, `review_required`, `block`, and `unverifiable`.
- Add a builder function that accepts an existing `replay.Report` and returns the compact focus report.
- Map replay verification and outcome states into focus verdicts conservatively:
  - Integrity failures, failed quality gates, failed policy checks, failed commands, or diff mismatch evidence map to `block`.
  - Unverifiable authenticity or untrusted signer evidence maps to `unverifiable` unless integrity also failed.
  - Warnings, unknown policy checks, missing gates, sensitive files, dependency files, generated files, or production-without-test changes map to `review_required`.
  - Fully finalized, valid, clean sessions map to `pass`.
- Include top-level fields required by the source document: `verdict`, `top_reasons`, `review_tasks`, `changed_files`, `failed_gates`, and `evidence_refs`.
- Cap `top_reasons` and `review_tasks` to small deterministic defaults, such as 5 top reasons and 20 tasks, while keeping ordering stable.
- Add unit tests in `internal/replay` that construct replay reports for pass, block, review-required, and unverifiable cases.
- Ensure the compact report does not include raw provider logs, raw prompts, raw tool output, or unredacted command output.

Acceptance Criteria:
- A replay report can be converted into a compact focus report without rebuilding session evidence.
- The compact report is deterministic for a fixed replay report.
- Verdict mapping is covered by unit tests.
- Focus report fields are JSON-serializable and omit empty optional fields where appropriate.
- No existing `replay` JSON field is removed or renamed.

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
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 2 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for the internal compact session focus report model.

Commit:
- `feat: add compact replay focus model`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for this completed step.

### Step 2: Add `agentreceipt focus --json`

Goal: Expose the compact focus report as the primary reviewer-agent loop command.

Depends on:
- Step 1

Changes:
- Add a new Cobra command in `cmd/root.go` named `focus`.
- Require explicit input through one of:
  - `agentreceipt focus --session <id> --json`
  - `agentreceipt focus --replay <path> --json`
- Reject implicit latest-session behavior for `focus`.
- Reuse existing `--repo`, `--config`, and trust-policy behavior where the focus command builds a replay report from a local session.
- When `--replay <path>` is used, load an existing `replay.json` file and derive the focus report from it without requiring local AgentReceipt session storage.
- Print compact JSON only when `--json` is set. If maintainers prefer the command to be JSON-only for the first release, make `--json` required and return a clear error otherwise.
- Add command-discovery tests in `cmd/root_test.go`.
- Add command behavior tests that cover missing input, mutually exclusive `--session` and `--replay`, valid session focus output, and valid replay-file focus output.
- Update `README.md` command listings and the replay workflow section with the new loop-oriented command.
- Update `docs/replay-evaluator-contract.md` with the relationship between `replay.json` and `focus --json`.

Acceptance Criteria:
- `agentreceipt focus --session <id> --json` emits `kind: "agentreceipt.session_focus"`.
- `agentreceipt focus --replay replay.json --json` emits the same focus payload for the same replay evidence.
- The command fails when neither `--session` nor `--replay` is provided.
- The command fails when both `--session` and `--replay` are provided.
- The output excludes raw provider logs, raw prompts, and raw tool outputs.
- Existing `agentreceipt replay`, `review`, `verify`, and `export` behavior remains unchanged.

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
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 3 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for `agentreceipt focus --json`.

Commit:
- `feat: add session focus command`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for this completed step.

### Step 3: Add ranked structured review tasks

Goal: Replace unstructured focus prompts in the compact focus report with ranked tasks that reviewer agents can execute directly.

Depends on:
- Step 1
- Step 2

Changes:
- Extend the internal focus report builder with `ReviewTask` records containing:
  - `id`
  - `priority`
  - `kind`
  - `question`
  - `paths`
  - `symbols`
  - `evidence_refs`
  - `confidence`
  - `source`
- Use priority values such as `P0`, `P1`, `P2`, and `P3`.
- Use task kinds such as `integrity_failure`, `authenticity_unverifiable`, `failed_gate`, `failed_command`, `risky_file`, `missing_test`, `diff_mismatch`, `dependency_change`, `sensitive_change`, `generated_change`, and `evidence_gap`.
- Rank tasks deterministically:
  - `P0` for integrity failure, diff mismatch, failed quality gates, failed commands, and failed policy checks.
  - `P1` for unverifiable authenticity, sensitive-file changes, dependency-file changes, destructive/network command evidence, and production changes without test changes.
  - `P2` for missing or unknown gates, generated files, CI/security file changes, and evidence gaps.
  - `P3` for informational review prompts.
- Map existing `replay.PolicyCheck`, `replay.QualityGate`, `replay.ReviewFocusItem`, `replay.FailedCommandDetail`, and `replay.PatchSummary` evidence into tasks.
- Ensure every task has stable IDs assigned after ranking, such as `task_001`, `task_002`, and so on.
- Add tests for task ranking, deterministic ordering, cap behavior, evidence refs, and path association.
- Update `docs/replay-evaluator-contract.md` and README examples to show structured review tasks.

Acceptance Criteria:
- Focus output includes ranked `review_tasks` with stable IDs.
- The same replay input produces the same task order and IDs across runs.
- Every non-informational task has at least one evidence ref when source evidence exists.
- Failed gates and failed commands produce higher-priority tasks than missing optional evidence.
- The builder never emits duplicate tasks for the same underlying reason and evidence set.

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
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 4 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for ranked reviewer-agent tasks in focus output.

Commit:
- `feat: rank reviewer focus tasks`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for this completed step.

### Step 4: Add per-file evidence dossiers

Goal: Help reviewer agents focus diff review by summarizing evidence for each changed file.

Depends on:
- Step 1
- Step 2
- Step 3

Changes:
- Extend focus changed-file entries with a dossier shape containing:
  - `path`
  - `action`
  - `category`
  - `sensitive`
  - `dependency`
  - `symbols`
  - `read_before_edit`
  - `related_context_read`
  - `tests_related`
  - `commands_touching_file`
  - `review_reasons`
  - `evidence_refs`
- Derive file identity from `replay.PatchSummary.ChangedFiles` and existing `replay.File` evidence.
- Derive `read_before_edit` and `related_context_read` from existing policy checks where possible.
- Associate command evidence with files conservatively by matching explicit path references in commands and evidence refs; do not infer command-file relationships from broad commands such as `go test ./...` unless the relationship is already clear.
- Populate `tests_related` from commands that mention the file, package directory, or test target. If the relation is broad or uncertain, leave the list empty and add a review reason instead of guessing.
- Add file-specific review reasons for sensitive files, dependency files, generated/unknown files, production files without test changes, CI/security files, failed command refs, and relevant policy warnings.
- Add tests for Go production file changes, docs-only changes, dependency changes, sensitive-file changes, and production-without-tests cases.
- Update `docs/replay-evaluator-contract.md` with the focus changed-file dossier fields.

Acceptance Criteria:
- Every changed file in the final patch has a focus changed-file dossier.
- Dossiers carry symbols already extracted by patch summary when available.
- Dossiers distinguish `pass`, `fail`, `warn`, `unknown`, and `not_applicable` evidence where applicable.
- Dossiers do not claim test coverage for a file unless evidence supports it.
- Review tasks can point to file dossiers through matching paths and evidence refs.

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
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 5 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for per-file evidence dossiers in focus output.

Commit:
- `feat: add per-file focus dossiers`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for this completed step.

### Step 5: Capture coding-agent instruction files at session start

Goal: Record which local coding-agent instructions were in force so reviewer loops can evaluate session behavior against them.

Depends on:
- Step 0

Changes:
- Add an instruction-capture module, for example under `internal/capture/instructions` or `internal/evidence`.
- Discover applicable instruction files at session start:
  - `AGENTS.md`
  - `CLAUDE.md`
  - Any existing repo convention already documented by maintainers before implementation begins.
- Start with repository-root discovery. Add parent-directory discovery only if it can be bounded safely and tested deterministically.
- For each discovered file, record a normalized event with:
  - relative path when inside the repo
  - absolute path redacted or omitted when outside the repo
  - sha256 hash
  - size
  - mtime
  - bounded deterministic summary of relevant rule categories, such as read-before-edit, validation, destructive-command restrictions, git restrictions, test-running restrictions, and privacy restrictions
- Do not store private keys, environment contents, raw provider transcripts, or raw prompt text as instruction evidence.
- Integrate capture into session start after repository resolution and before provider or filesystem events begin.
- Include instruction evidence in replay gaps and claims when absent or unreadable.
- Add replay and focus fields such as `instruction_files` or `instruction_evidence` in an additive way.
- Add tests for no instruction files, one repo instruction file, unreadable instruction file, and summary extraction.
- Update README and `docs/replay-evaluator-contract.md` to describe instruction capture privacy and limitations.

Acceptance Criteria:
- Starting a session records instruction metadata when `AGENTS.md` or `CLAUDE.md` exists.
- Missing instruction files are not treated as failure.
- Unreadable instruction files produce a warning or gap, not a crash.
- Replay and focus output can state which instruction files were captured.
- The event log does not store raw prompt/session text.

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
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 6 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for coding-agent instruction capture.

Commit:
- `feat: capture coding agent instruction evidence`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for this completed step.

### Step 6: Separate pre-existing and agent-introduced changes

Goal: Prevent reviewer loops from treating unrelated dirty workspace state as session-introduced work.

Depends on:
- Step 0

Changes:
- Inspect existing git snapshot events and session state to identify what is already captured at session start and stop.
- Add deterministic replay extraction for:
  - `pre_existing_dirty_files`
  - `agent_touched_pre_existing_files`
  - `agent_created_changes`
  - `agent_modified_clean_files`
  - `final_diff_matches_workspace`
  - `final_diff_matches_branch`
- Prefer existing git monitor evidence before adding new event fields. Add new event payload fields only if existing start/final snapshots are insufficient.
- Add additive replay fields under a new grouping such as `workspace_change_summary`.
- Surface the same summary in focus output and review tasks.
- Mark pre-existing dirty files as review context, not automatic blockers.
- Mark final diff mismatches as blocker tasks when evidence is high confidence.
- Add tests for clean start, dirty start with unrelated file, dirty start with agent-touched file, untracked file at start, and final diff mismatch.
- Update README and `docs/replay-evaluator-contract.md` with the distinction between pre-existing and session-introduced changes.

Acceptance Criteria:
- Replay and focus output distinguish files dirty before session start from files changed during the session.
- Agent-touched pre-existing files are highlighted separately.
- Unrelated pre-existing dirty files do not cause a block verdict by themselves.
- Final diff mismatch remains a high-priority blocker.
- Existing review output is not regressed.

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
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 7 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for start-state versus session-introduced change reporting.

Commit:
- `feat: separate pre-existing workspace changes`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for this completed step.

### Step 7: Add stable JSON schema output

Goal: Let reviewer loops validate AgentReceipt JSON payloads without scraping docs or source code.

Depends on:
- Step 1
- Step 2

Changes:
- Add JSON Schema files for replay and focus, for example:
  - `docs/schemas/replay.schema.json`
  - `docs/schemas/focus.schema.json`
- Add a `schema` command to `cmd/root.go`:
  - `agentreceipt schema replay`
  - `agentreceipt schema focus`
- Ensure schema output is deterministic and valid JSON.
- Keep schemas additive and compatible with existing replay compatibility rules.
- Include enum values for focus verdicts, task priorities, task kinds, quality gate statuses, policy check statuses, outcome statuses, and verification statuses.
- Add tests that assert both schema commands exist, emit valid JSON, and contain the expected `kind` and top-level required fields.
- Update README and `docs/replay-evaluator-contract.md` with schema command examples.

Acceptance Criteria:
- `agentreceipt schema replay` prints a valid JSON Schema for replay payloads.
- `agentreceipt schema focus` prints a valid JSON Schema for focus payloads.
- The focus schema validates the contract introduced by Steps 1 through 4.
- The replay schema documents existing replay fields without removing compatibility fields.
- Schema files do not depend on network access.

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
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 8 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for replay and focus JSON schema output.

Commit:
- `feat: expose replay and focus schemas`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for this completed step.

### Step 8: Add machine-oriented exit codes for loop-facing commands

Goal: Let shells and reviewer agents branch on deterministic command results without parsing prose.

Depends on:
- Step 2
- Step 7

Changes:
- Define exit-code constants for loop-facing commands:
  - `0` for clean pass
  - `10` for review required
  - `20` for failed quality gate or blocker evidence
  - `30` for integrity failure
  - `40` for authenticity unverifiable
  - `50` for receipt diff mismatch
  - `60` for invalid input
- Apply these exit codes first to `agentreceipt focus` and later to `agentreceipt verify diff` in Step 9.
- Preserve existing command behavior unless the command is explicitly loop-facing.
- Ensure JSON is still written for known evidence verdicts before exiting non-zero, except for invalid input cases where no valid report can be built.
- Add command tests that execute focus scenarios and assert the returned error maps to the intended process exit code.
- If the current `main.go` command execution path does not support custom exit codes, add a minimal typed error such as `cmd.ExitError` and handle it at the top-level process boundary.
- Document exit codes in README and `docs/replay-evaluator-contract.md`.

Acceptance Criteria:
- `focus --json` exits `0` for pass.
- `focus --json` exits `10` for review-required evidence.
- `focus --json` exits `20` for blocker evidence such as failed gates or failed commands.
- `focus --json` exits `30` for integrity failure.
- `focus --json` exits `40` for unverifiable authenticity when integrity is otherwise valid.
- `focus --json` exits `50` for high-confidence diff mismatch evidence.
- Invalid command usage exits `60` or maps clearly to the existing CLI invalid-input behavior if Cobra constraints make a separate code impractical.
- Exit-code behavior is documented and tested.

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
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 9 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for deterministic loop-facing exit codes.

Commit:
- `feat: add focus exit codes`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for this completed step.

### Step 9: Add first-class diff equivalence verification

Goal: Give reviewer loops a hard local gate for whether the receipt final patch matches the diff under review.

Depends on:
- Step 8

Changes:
- Add a subcommand under `verify`, for example:
  - `agentreceipt verify diff --session <id> --against merge-base`
  - `agentreceipt verify diff --session <id> --against HEAD`
  - `agentreceipt verify diff --bundle <path> --against patch:<path>`
  - `agentreceipt verify diff --bundle <path> --against pr.patch`
- Reuse existing artifact verification before comparing diffs.
- Compare normalized patch content in a deterministic way. Document any normalization limits clearly.
- Support at least:
  - local finalized session final patch versus current workspace diff
  - local finalized session final patch versus branch diff from merge base
  - bundle final patch versus supplied patch file
- Return machine-oriented exit codes:
  - `0` when equivalent
  - `30` when receipt or bundle integrity fails
  - `50` when diffs do not match
  - `60` for invalid inputs
- Add JSON output if practical, for example `--json` with fields `equivalent`, `against`, `final_patch_hash`, `candidate_patch_hash`, `reason`, and `evidence_refs`.
- Add tests for matching diff, mismatching diff, invalid bundle, missing patch file, and unsupported `--against` value.
- Update README and `docs/GITHUB_PR_WORKFLOW_DESIGN.md` with the local diff verification workflow.

Acceptance Criteria:
- Review loops can verify final patch parity without reading raw receipt internals.
- Diff mismatch is clearly distinguishable from integrity failure.
- Bundle and local-session paths share as much verification code as practical.
- Existing `agentreceipt verify` and `verify bundle` behavior remains unchanged.

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
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 10 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for local receipt diff equivalence verification.

Commit:
- `feat: verify receipt diff equivalence`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for this completed step.

### Step 10: Add loop-health evaluator signals

Goal: Expose factual signals that help reviewer agents identify unstable or low-confidence coding sessions without introducing a score.

Depends on:
- Step 1
- Step 2

Changes:
- Extend replay `EvaluatorSignals` and focus output with loop-health fields such as:
  - `total_tokens`
  - `failed_command_streak`
  - `same_file_edit_count`
  - `read_to_edit_ratio`
  - `validation_after_last_edit`
  - `last_edit_time`
  - `last_validation_time`
- Derive `total_tokens` from provider evidence token totals when available.
- Derive failed command streaks from ordered command result evidence.
- Derive same-file edit counts from filesystem watcher and final patch evidence conservatively.
- Derive read-to-edit ratio from command classifications already used by policy checks.
- Derive validation-after-last-edit from validation command evidence ordered after the last edit/write evidence.
- Add review tasks when loop-health signals indicate obvious review needs, such as failed command streaks or no validation after the last edit.
- Keep these as factual counters and booleans, not scores.
- Add tests for token extraction, failed-command streaks, validation after last edit, no command evidence, and repeated file edits.
- Update `docs/replay-evaluator-contract.md` and README with loop-health semantics.

Acceptance Criteria:
- Replay and focus output include factual loop-health signals when evidence supports them.
- Missing provider token evidence results in unknown or zero-value fields with appropriate confidence, not failure.
- Loop-health signals do not affect receipt integrity verification.
- Loop-health signals can add review tasks but do not create an opaque session score.

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
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 11 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for loop-health evaluator signals.

Commit:
- `feat: add reviewer loop health signals`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for this completed step.

### Step 11: Add evidence reference dereferencing

Goal: Let reviewer agents resolve evidence refs directly from replay or focus output without manually parsing `events.jsonl`.

Depends on:
- Step 2
- Step 3
- Step 4

Changes:
- Add an additive evidence index to replay and focus output, for example `evidence_index`.
- Include compact entries for event refs, artifact refs, command refs, file refs, and diff refs.
- Each evidence entry should include:
  - `ref`
  - `type`
  - `summary`
  - `time`
  - `command`
  - `exit_code`
  - `path`
  - `artifact`
  - `confidence`
  - `redacted`
- Populate only fields relevant to the evidence type.
- Redact command output summaries using existing replay redaction and cap behavior.
- Keep raw provider logs out of replay and focus output.
- Ensure every `review_task.evidence_refs` and file dossier evidence ref can be found in `evidence_index` when the referenced evidence is local to the replay report or bundle.
- Add tests for event refs, failed command refs, final patch refs, missing refs, redaction, stable ordering, and bundle output.
- Update `docs/replay-evaluator-contract.md` with evidence index semantics.

Acceptance Criteria:
- Reviewer agents can resolve common evidence refs without reading `events.jsonl` directly.
- Evidence index entries are compact and privacy-preserving.
- Existing refs remain stable.
- Missing or external refs are represented clearly instead of silently disappearing.
- Replay bundles include the dereferenceable evidence index in `replay.json`.

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
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 12 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for dereferenceable replay and focus evidence refs.

Commit:
- `feat: add replay evidence index`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for this completed step.

### Step 12: Final documentation and contract hardening

Goal: Consolidate the reviewer-loop workflow and make the new contracts safe for other agents to consume.

Depends on:
- Step 1
- Step 2
- Step 3
- Step 4
- Step 5
- Step 6
- Step 7
- Step 8
- Step 9
- Step 10
- Step 11

Changes:
- Update README with a new coding-agent reviewer-loop workflow section showing:
  - `agentreceipt replay --session <id> --json`
  - `agentreceipt focus --session <id> --json`
  - `agentreceipt focus --replay replay.json --json`
  - `agentreceipt schema replay`
  - `agentreceipt schema focus`
  - `agentreceipt verify diff --session <id> --against merge-base`
- Update `docs/replay-evaluator-contract.md` with the final additive contract:
  - focus report
  - review tasks
  - file dossiers
  - instruction evidence
  - workspace change separation
  - exit codes
  - diff equivalence
  - loop-health signals
  - evidence index
- Update `docs/PRD.md` to reflect implemented scope once the behavior is complete.
- Update `docs/TECH_SPEC.md` to record the internal architecture and privacy constraints.
- Update smoke coverage in `scripts/smoke.sh` for the most important new happy paths:
  - `focus --session --json`
  - `schema focus`
  - `schema replay`
  - a basic `verify diff` path
- Review CLI help text for concise, loop-oriented wording.
- Ensure changelog entries from all prior steps are coherent and grouped under `## [Unreleased]`.

Acceptance Criteria:
- README explains when to use `replay`, `focus`, `schema`, and `verify diff`.
- Contract docs describe every new machine-readable field and enum.
- PRD and TECH_SPEC no longer describe the implementation as future-only where it now exists.
- Smoke tests cover the new loop-facing command surface.
- No docs imply that AgentReceipt ranks agents, scores models, enforces policy, or orchestrates agents.

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
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and final completion status.

Changelog:
- Finalize `CHANGELOG.md` `## [Unreleased]` entries for the reviewer-loop command surface and contract hardening.

Commit:
- `docs: document coding agent reviewer loops`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for this completed step.

## Suggested Execution Order

- Step 0 must be completed first.
- Steps 1 through 4 should land together conceptually but as separate commits: model, CLI, tasks, and file dossiers.
- Step 5 and Step 6 can proceed after Step 0 and can be parallelized by separate executors if they avoid touching the same files.
- Step 7 should land after the focus model exists.
- Step 8 should land after the focus command and schemas are stable enough to document exit-code behavior.
- Step 9 depends on exit-code handling but can otherwise be implemented independently of loop-health signals and evidence indexing.
- Step 10 and Step 11 can proceed after the focus and replay additions are stable.
- Step 12 must be last because it reconciles documentation, smoke coverage, and changelog entries across the full feature set.

## Rollback and Compatibility Notes

- All replay schema changes must be additive. Do not remove or rename existing fields.
- Keep `review` Markdown and terminal output stable unless a step explicitly updates documentation and tests for a visible change.
- If `focus` contract design becomes contentious, hide it behind a documented experimental warning rather than mixing unstable fields into existing `replay` output.
- If instruction capture raises privacy concerns, keep only hashes and file paths in the first release and defer rule summaries.
- If custom process exit codes require invasive CLI changes, restrict them to `focus` and `verify diff` and document any limitations before broadening.
- If diff normalization produces false mismatches on real patches, return a `review_required` or mismatch-with-low-confidence result instead of claiming equivalence.
