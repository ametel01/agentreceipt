# AI Agent Command Improvements Progress

Source documents:
- `/Users/alexmetelli/source/agentreceipt/PLAN.md`
- `docs/AI_AGENT_COMMAND_IMPROVEMENTS.md`
- `README.md`
- `Makefile`

## Step Checklist

- [x] Step 0: Progress and Changelog Tracking Setup
- [x] Step 1: Add shared agent-loop contract primitives
- [x] Step 2: Add ranked focus work queues and file classifications
- [x] Step 3: Add compact replay indexes and query surfaces
- [x] Step 4: Wire reviewability output and harden documentation

## Status

- Current phase: `Step 4` completed
- Next step: `complete`
- Rule: `PROGRESS.md` is updated after each completed step, including validation results, commit reference if available, current status, and next step.

## Update Log

- 2026-06-21 — Initialized plan-specific progress tracking for the AI agent command improvements work.
  - Reframed progress tracking around `PLAN.md` and the agent-loop contract changes.
  - Marked Step 0 complete and set Step 1 as next.
  - Validation: pending initial step setup.
  - Commit: `5b5ec40`

- 2026-06-21 — Completed Step 1 for shared agent-loop contract primitives.
  - Added shared `ReasonCode`, `ProcessContract`, and `Reviewability` types in `internal/replay` and wired them into replay/focus JSON output.
  - Added deterministic `reason_code` fields to replay `review_focus` items and focus review reasons/tasks.
  - Added schema coverage, contract tests, and CLI smoke coverage for the new top-level contract fields.
  - Validation:
    - `go test ./internal/replay`
    - `go test ./internal/replay ./cmd`
    - `make fmt-check`
    - `make tools`
    - `make lint`
    - `make test`
    - `make build`
    - `make smoke`
    - `make verify`
  - Commit: `ae904ee`

- 2026-06-21 — Step 3 is next.
  - Replay index and query-surface work remained to be implemented from `PLAN.md`.
  - Current tracker state is ready for the next feature commit.

- 2026-06-21 — Completed Step 3 for compact replay indexes and query surfaces.
  - Added compact replay output with `indexes`, `query`, and optional `selected_events` / `selected_files` / `selected_evidence` slices, plus `--full` timeline output.
  - Added replay index projection tests, CLI query/full-mode coverage, schema updates, smoke coverage, and replay contract documentation for the new compact default.
  - Validation:
    - `go test ./internal/replay`
    - `go test ./cmd`
    - `go test ./...`
    - `./scripts/smoke.sh`
    - `make verify`
  - Commit: `f573a36`

- 2026-06-21 — Completed Step 2 for ranked focus work queues and file classifications.
  - Added `agent_tasks`, `recommended_next_commands`, `reviewable_files`, and `suppressed_changes` to focus output.
  - Added stable file classification buckets, suppression handling for transient artifacts, and stronger task deduplication/ranking keyed by kind, gate, path, and reason code.
  - Added focused tests for distinct gate deduplication, agent-task queue generation, recommended command emission, and file suppression/classification.
  - Validation:
    - `go test ./internal/replay`
    - `go test ./internal/replay ./cmd`
    - `make verify`
  - Commit: `d15035f`

- 2026-06-21 — Completed Step 4 for reviewability output and documentation hardening.
  - Added explicit `reviewability` references to the replay/focus docs, updated the focus docs schema to match the live contract, and aligned the product/technical specs with the implemented replay and focus behavior.
  - Validation:
    - `make verify`
  - Commit: `6e692ea`
