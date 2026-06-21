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
- [ ] Step 5: Capture coding-agent instruction files at session start
- [ ] Step 6: Separate pre-existing and agent-introduced changes
- [ ] Step 7: Add stable JSON schema output
- [ ] Step 8: Add machine-oriented exit codes for loop-facing commands
- [ ] Step 9: Add first-class diff equivalence verification
- [ ] Step 10: Add loop-health evaluator signals
- [ ] Step 11: Add evidence reference dereferencing
- [ ] Step 12: Final documentation and contract hardening

## Status

- Current phase: `Step 4` completed
- Next step: `Step 5`
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
