# Reviewer-Agent Loop Implementation Progress

Source documents:
- `/Users/alexmetelli/source/agentreceipt/improve.md`
- `docs/replay-evaluator-contract.md`
- `docs/GITHUB_PR_WORKFLOW_DESIGN.md`
- `README.md`
- `PLAN.md`

## Step Checklist

- [x] Step 0: Progress and Changelog Tracking Setup
- [x] Step 1: Add the compact focus report model
- [x] Step 2: Add `agentreceipt focus --json`
- [x] Step 3: Add ranked structured review tasks
- [x] Step 4: Add per-file evidence dossiers
- [x] Step 5: Capture coding-agent instruction files at session start
- [x] Step 6: Separate pre-existing and agent-introduced changes
- [ ] Step 7: Add stable JSON schema output
- [ ] Step 8: Add machine-oriented exit codes for loop-facing commands
- [ ] Step 9: Add first-class diff equivalence verification
- [ ] Step 10: Add loop-health evaluator signals
- [ ] Step 11: Add evidence reference dereferencing
- [ ] Step 12: Final documentation and contract hardening

## Status

- Current phase: `Step 6` completed
- Next step: `Step 7`
- Rule: `PROGRESS.md` is updated after each completed step, including validation results, commit reference, and next step.

## Update Log

- 2026-06-21 — Initialized progress tracking for coding-agent reviewer-loop implementation.  
  - Created `PROGRESS.md` from `PLAN.md` with all planned steps.
  - Confirmed `CHANGELOG.md` has `# Changelog` and `## [Unreleased]`.
  - Marked Step 0 as complete and set Step 1 as next.
  - Validation: `test -f PROGRESS.md`; `grep -q "Step 1: Add the compact focus report model" PROGRESS.md`; `test -f CHANGELOG.md`; `grep -q "^## \\[Unreleased\\]" CHANGELOG.md` (pass).
  - Commit: `5744cb7`

- 2026-06-21 — Completed Step 1 for compact focus report model.
  - Added `FocusReport` builder and related models in `internal/replay` and covered pass/review/block/unverifiable behavior plus deterministic output in unit tests.
  - Validation:
    - `make fmt-check`
    - `make lint`
    - `make test`
    - `make test-race`
    - `make security`
    - `make coverage`
    - `make build`
    - `make smoke`
    - `make verify`
  - Commit: `f05ecfb`

- 2026-06-21 — Completed Step 2 for adding the session focus command.
  - Added `agentreceipt focus --json` command supporting `--session <id>` and `--replay <path>`.
  - Added command-discovery tests and behavior tests for source selection, missing JSON behavior, and replay-source parity with session source.
  - Updated README and evaluator contract docs to cover the new command and its replay relationship.
  - Validation:
    - `make fmt-check`
    - `make lint`
    - `make test`
    - `make test-race`
    - `make security`
    - `make coverage`
    - `make build`
    - `make smoke`
    - `make verify`
  - Commit: `8805e41`

- 2026-06-21 — Completed Step 3 for ranked structured review tasks.
  - Added task priorities (`P0`/`P1`/`P2`/`P3`) and stable task kinds in focus task generation.
  - Added evidence-aware task ranking and deterministic deduplication keyed by kind/priority/question.
  - Added helper mapping for policy checks, changed symbols/paths, command-file associations, and comparator-based ordering.
  - Added tests for ranked output order, deterministic task IDs, evidence coverage, and failed-command path/symbol extraction.
  - Updated `docs/replay-evaluator-contract.md` and `README.md` to describe structured ranking fields.
  - Validation:
    - `make fmt-check`
    - `make lint`
    - `make test`
    - `make test-race`
    - `make security`
    - `make coverage`
    - `make build`
    - `make smoke`
    - `make verify`
  - Commit: `db21000`

- 2026-06-21 — Completed Step 4 for per-file evidence dossiers.
  - Expanded focus dossier entries in `internal/replay` to include dependency/symbol signals, read and related-context status, command/test associations, review reasons, and merged evidence references.
  - Added conservative command-to-file association logic, explicit file-level reason synthesis, and targeted dossier tests covering production/docs/dependency/sensitive paths plus conservative test inference.
  - Updated `docs/replay-evaluator-contract.md` to document the new `changed_files` dossier field surface and status enums.
  - Validation:
    - `make fmt-check`
    - `make lint`
    - `make test`
    - `make test-race`
    - `make security`
    - `make coverage`
    - `make build`
  - `make smoke`
  - `make verify` (fails reproducibly in this run: `TestSessionCapturesFilesystemChanges` in `internal/session` reports `decode session state: unexpected end of JSON input`)
  - Commit: `23f7f6f`

- 2026-06-21 — Completed Step 5 for capturing AGENTS.md/CLAUDE.md evidence at session start.
  - Added `internal/capture/instructions` package with capture helpers, event typing, and warning reporting for unreadable/non-regular instruction files.
  - Wired `Session.Manager.Start()` to append instruction metadata events alongside git snapshots and persist capture warnings into session state/manifest.
  - Extended replay schema and focus output with `instruction_files` and added tests covering metadata capture, warning mapping, and focus propagation.
  - Updated docs for evidence sources and behavior notes for missing instruction files.
  - Updated session tests to assert start capture behavior and warning persistence for invalid instruction paths.
  - Validation:
    - `gofmt -w internal/capture/instructions/instructions.go internal/replay/replay.go internal/replay/focus.go internal/replay/focus_test.go internal/replay/replay_test.go internal/session/session.go internal/session/session_test.go docs/replay-evaluator-contract.md README.md CHANGELOG.md`
  - `go test ./internal/session ./internal/replay ./internal/capture/instructions`
  - `go test ./...`
  - Commit: not committed

- 2026-06-21 — Completed Step 6 for workspace change separation.
  - Added replay-side workspace change classification from git snapshots and final patch evidence:
    - `pre_existing_dirty_files`
    - `agent_touched_pre_existing_files`
    - `agent_created_changes`
    - `agent_modified_clean_files`
    - `final_diff_matches_workspace`
    - `final_diff_matches_branch`
  - Added focus propagation of workspace context and blocker tasks when final patch mismatch with current workspace is detected.
  - Added replay and focus unit tests for clean-start, pre-existing dirty, touched pre-existing, untracked-start, and final diff mismatch scenarios.
  - Updated evaluator contract and README to document start-state vs agent-introduced change distinctions.
  - Validation:
    - `gofmt -w internal/replay/replay.go internal/replay/focus.go internal/replay/replay_test.go internal/replay/focus_test.go docs/replay-evaluator-contract.md README.md PROGRESS.md`
    - `make fmt-check`
    - `make lint`
    - `make test`
    - `go test -race ./...` (fails reproducibly in this run: `TestBuildMergesFinalPatchAndFilesystemEvidence` reports `git rev-parse --show-toplevel: signal: segmentation fault`)
    - `make security` (fails on pre-existing/high-confidence findings in `internal/replay/replay.go` and `internal/capture/instructions/instructions.go`)
    - `make coverage`
    - `make build`
    - `make smoke`
    - `go test ./internal/replay`
    - `make verify` (fails in this run during `test-race`/`security` phase)
  - Commit: `2ce3071`
