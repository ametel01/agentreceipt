# Implementation Plan

## Source Documents
- Path: `docs/AI_AGENT_COMMAND_IMPROVEMENTS.md`
  - Role: Primary design note.
  - Summary: Proposes agent-facing improvements to `focus` and `replay`, including structured reason codes, a stable process contract, ranked agent tasks, compact next-command recommendations, replay indexing/query surfaces, and a top-level reviewability object.
- Path: `README.md`
  - Role: Current CLI surface and user-facing behavior context.
  - Summary: Describes the existing command set and the output contracts that must remain compatible unless the plan explicitly changes them.
- Path: `Makefile`
  - Role: Repository validation surface.
  - Summary: Defines the exact format, lint, test, race, security, coverage, build, smoke, and verify commands that should be used as quality gates.

## Goals
- Add structured agent-loop metadata to `focus` and `replay` without turning either command into a policy engine.
- Keep `focus` bounded, task-oriented, and easy for another agent to consume directly.
- Make `replay` more queryable and token-efficient by default while preserving access to full evidence artifacts.
- Introduce a stable process contract and reviewability object so downstream automation can decide whether to continue, rerun validation, or escalate.
- Preserve deterministic, local-first, privacy-preserving behavior and avoid breaking existing receipt consumers.

## Non-Goals
- Do not build an orchestrator, hosted service, or remote execution wrapper.
- Do not introduce trust scores, agent rankings, or opaque policy enforcement.
- Do not upload raw prompts, raw provider logs, unredacted tool output, or private keys by default.
- Do not remove or rename existing replay fields unless a step explicitly requires an additive compatibility change.
- Do not change unrelated CLI surfaces beyond what is needed to support the focus/replay improvements.

## Assumptions and Open Questions
- Assumption: New contract fields should be additive and backward-compatible wherever possible. Impact: downstream tools can adopt the new surface incrementally.
- Assumption: `focus` remains a bounded review queue and never emits the full event timeline. Impact: keeps agent responses small and deterministic.
- Assumption: `replay` can add compact/default modes and targeted query flags without losing access to the full evidence set. Impact: preserves forensic usefulness while improving loop performance.
- Assumption: Structured reason codes should be shared between `focus`, `replay`, and any schema output so the same condition is represented consistently. Impact: reduces duplication and drift.
- Open question: Should `--compact` become the default replay mode immediately, or should it ship as an explicit option before any default changes? Conservative choice: introduce the compact mode and query surfaces first, then decide on defaulting once coverage is stable.
- Open question: Which exact `reviewability.status` vocabulary should be used? Conservative choice: keep the smallest set that maps cleanly to current verdict semantics and avoids ambiguous overlap.
- Open question: Should the process contract live in a shared internal model first or be embedded directly into the JSON payloads from the start? Conservative choice: define the shared model first, then wire it into both commands.

## Quality Gates
- Setup status: Existing gates are already configured in `Makefile`; no extra gate scaffolding is required before implementation.
- Baseline command: `make tools && make verify`
- Format command: `make fmt-check`
- Lint command: `make lint`
- Test command: `make test`
- Additional gates: `make test-race`, `make security`, `make coverage`, `make build`, `make smoke`, `make verify`
- Full gate command: `make verify`
- Formatting repair command: `make fmt`
- Notes: `make verify` writes expected local artifacts such as `coverage.out` and `./agentreceipt`.

## Progress Tracking
- File: `PROGRESS.md`
- Requirement: Create `PROGRESS.md` before any quality-gate setup or implementation work begins.
- Update rule: After each completed step, update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.

## Changelog Tracking
- File: `CHANGELOG.md`
- Standard: Keep a Changelog 1.0.0, <https://keepachangelog.com/en/1.0.0/>
- Requirement: Create `CHANGELOG.md` before any quality-gate setup or implementation work begins.
- Initial content: Include `# Changelog`, the standard preamble, and an `## [Unreleased]` section.
- Update rule: After each step is completed and validated, update `CHANGELOG.md` with human-readable notable changes under the appropriate `Unreleased` change-type headings before creating that step's commit.

## Incremental Steps

### Step 0: Progress and Changelog Tracking Setup
Goal: Create plan-specific progress tracking and changelog tracking before any implementation work begins.

Depends on:
- None

Changes:
- Create or replace `PROGRESS.md` in the project root.
- Add the plan title, source documents, step checklist, current status, and a short update log.
- Document that `PROGRESS.md` must be updated after every completed step.
- Create or refresh `CHANGELOG.md` in the project root before implementation starts.
- Add Keep a Changelog 1.0.0 structure: `# Changelog`, the standard preamble, and `## [Unreleased]`.
- Document that `CHANGELOG.md` must be updated after each completed and validated step before that step is committed.

Acceptance Criteria:
- `PROGRESS.md` exists and contains every step from this plan.
- `PROGRESS.md` identifies Step 1 as the next implementation step.
- `CHANGELOG.md` exists and contains `# Changelog` plus `## [Unreleased]`.
- Existing release sections in `CHANGELOG.md` are preserved.

Validation:
- `test -f PROGRESS.md`
- `grep -q "Step 1: Add shared agent-loop contract primitives" PROGRESS.md`
- `test -f CHANGELOG.md`
- `grep -q "^## \\[Unreleased\\]" CHANGELOG.md`

Progress:
- Mark Step 0 complete in `PROGRESS.md`, record validation results, set the current status, and identify Step 1 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for establishing progress and changelog tracking for the agent-command improvement plan.

Commit:
- `docs: initialize agent-command improvement tracking`

Required End-of-Step Actions:
- Run all listed validation commands for this step.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with the notable completed work under `## [Unreleased]`.
- Create a commit for this completed step.

### Step 1: Add shared agent-loop contract primitives
Goal: Define the shared data model for structured reason codes, process contracts, and reviewability metadata before expanding the command payloads.

Depends on:
- Step 0

Changes:
- Add shared internal types for:
  - structured reason codes
  - process contract metadata
  - reviewability status
  - routing or blocker enums used by both `focus` and `replay`
- Keep the types additive and deterministic so they can be embedded in the existing JSON contracts.
- Add JSON/schema coverage for the new shared model fields where schemas are already emitted.
- Add unit tests for serialization, zero-value behavior, and stable enum encoding.

Acceptance Criteria:
- The shared model compiles without changing existing command behavior.
- Structured enums and reason codes serialize deterministically.
- The new process contract and reviewability fields are available to both focus and replay builders.
- No existing replay/focus fields are removed or renamed.

Validation:
- `make fmt-check`
- `make lint`
- `make test`
- `make test-race`
- `make security`
- `make coverage`
- `make build`
- `make smoke`
- `make verify`

Progress:
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 2 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for the shared agent-loop contract primitives.

Commit:
- `feat: add shared agent-loop contract primitives`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for that completed step.

### Step 2: Add ranked focus work queues and file classifications
Goal: Make `focus` emit a bounded work queue with ranked tasks, next-command guidance, and file-classification output that another agent can act on directly.

Depends on:
- Step 1

Changes:
- Extend the focus report builder with:
  - `agent_tasks`
  - `recommended_next_commands`
  - `reviewable_files`
  - `suppressed_changes`
- Deduplicate tasks using stable keys such as kind, gate, path, and reason code.
- Separate `source_changes`, `test_changes`, `doc_changes`, `generated_changes`, and `transient_changes`.
- Add stable ranking and ordering for tasks and reviewable files.
- Keep the report bounded and omit full event timelines.
- Add focused unit tests for deduplication, ranking, suppression, and file classification.

Acceptance Criteria:
- `focus` produces a compact work queue that is stable for a fixed replay input.
- Duplicate tasks collapse to one record with deterministic ordering.
- Low-review-value artifacts are suppressed without losing the ability to surface them under `suppressed_changes`.
- The report remains JSON-serializable and bounded.

Validation:
- `make fmt-check`
- `make lint`
- `make test`
- `make test-race`
- `make security`
- `make coverage`
- `make build`
- `make smoke`
- `make verify`

Progress:
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 3 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for ranked focus work queues and file classifications.

Commit:
- `feat: rank focus work queues`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for that completed step.

### Step 3: Add compact replay indexes and query surfaces
Goal: Make `replay` more queryable and token-efficient by default while keeping full evidence accessible through explicit artifact references.

Depends on:
- Step 1
- Step 2

Changes:
- Add replay indexes and artifact references for events, files, and evidence.
- Add targeted query surfaces such as event ranges, file filters, and evidence refs.
- Add `--full` for exhaustive replay output and `--compact` for the agent-friendly default shape.
- Normalize event types and distinguish observed facts from inferred claims.
- Represent missing evidence as structured data instead of repeated prose.
- Add tests for compact output, query selection, index stability, and evidence reference resolution.

Acceptance Criteria:
- The replay output is compact by default and queryable without loading the full timeline inline.
- The full evidence record remains reachable through artifact references and explicit query flags.
- Query selection is deterministic for the same input.
- Structured missing-evidence handling is preserved in JSON output.

Validation:
- `make fmt-check`
- `make lint`
- `make test`
- `make test-race`
- `make security`
- `make coverage`
- `make build`
- `make smoke`
- `make verify`

Progress:
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and Step 4 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for compact replay indexes and query surfaces.

Commit:
- `feat: compact replay output and queries`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for that completed step.

### Step 4: Wire reviewability output and harden documentation
Goal: Propagate the top-level reviewability object through focus and replay, then harden the user-facing documentation and contracts.

Depends on:
- Step 1
- Step 2
- Step 3

Changes:
- Add the top-level `reviewability` object to the relevant JSON payloads.
- Ensure reviewability reflects the structured contract, task queue, and replay index state consistently.
- Update CLI help, README examples, replay contract docs, and schema references to match the final behavior.
- Add smoke coverage or contract tests for the final loop-facing command surface.
- Confirm the final output remains deterministic, additive, and privacy-preserving.

Acceptance Criteria:
- Reviewability metadata is visible in the loop-facing outputs and matches the underlying report state.
- The documentation matches the implemented behavior and examples use the current command syntax.
- The final command surface remains backward-compatible for existing consumers where compatibility is promised.

Validation:
- `make fmt-check`
- `make lint`
- `make test`
- `make test-race`
- `make security`
- `make coverage`
- `make build`
- `make smoke`
- `make verify`

Progress:
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and `complete` as the next step.

Changelog:
- Add an `Added` or `Changed` entry under `## [Unreleased]` for the final reviewability and documentation hardening work.

Commit:
- `docs: harden agent-loop contracts`

Required End-of-Step Actions:
- Run all quality gates: format, lint, tests, and project-specific checks.
- Fix any failures before proceeding.
- Update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.
- Update `CHANGELOG.md` with notable completed work under `## [Unreleased]`, using the appropriate Keep a Changelog change-type heading.
- Create a commit for that completed step.
