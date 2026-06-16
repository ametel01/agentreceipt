# Implementation Plan

## Source Documents
- Path: Inline user decision from 2026-06-17 conversation
  - Role: Primary implementation decision
  - Summary: Use `github.com/rs/zerolog` as the production-grade structured logger for `start --watch`, model watch output as structured events, render human-readable colored console output with `zerolog.ConsoleWriter`, add `--color auto|always|never`, and defer a broader CLI logging replacement until the watch path is proven.

## Goals
- Replace the current ad hoc `fmt.Fprintf` watch rendering with a zerolog-backed watch logger/formatter.
- Preserve compact human-readable `start --watch` output while routing each rendered line through a structured `WatchEvent` shape.
- Add color-mode control with `--color auto|always|never`, defaulting to `auto`.
- Keep the implementation scoped to Codex watch output first, leaving room for a later `--log-format console|json` flag without redesigning the renderer.
- Maintain the existing `start --watch` behavior: repo-aware Codex log selection, provider-event import, concise command/edit/token/warning output, and truncation of long subjects.

## Non-Goals
- Do not replace every CLI `fmt.Fprint/Fprintf/Fprintln` call with zerolog.
- Do not add `--log-format console|json` in this first pass; design for it, but keep the user-facing flag surface limited to `--color`.
- Do not change Codex JSONL parsing semantics, event-log persistence, receipt signing, review rendering, or export formats except where tests need fixture updates for watch output.
- Do not add new hosted logging, telemetry upload, or external log sink behavior.

## Assumptions and Open Questions
- Assumption: `--color` should be a global persistent flag so future commands can reuse the policy, but only `start --watch` must consume it in this plan.
- Assumption: `auto` means use colors only when stdout is a terminal; tests should force deterministic `never`/`always` modes rather than depending on terminal detection.
- Assumption: watch output remains line-oriented and human-readable by default, matching the target shape `codex  ok      run make verify (exit 0)` without JSON unless a future `--log-format` flag is added.
- Assumption: zerolog fields should carry structured values even when the console writer hides most of them; this keeps the future JSON path cheap.
- Open question: exact terminal detection dependency. Conservative choice: use Go standard library plus a small terminal helper only if needed, and avoid adding a second logging/UI package.
- Open question: whether warning label text should be `warn` everywhere. Conservative choice: normalize existing watch warnings from `warning` to `warn` because the decision's target output uses `warn`.

## Quality Gates
- Setup status: Existing gates are configured in `Makefile`, `.golangci.yml`, and `.github/workflows/ci.yml`; no separate quality-gates setup step is required.
- Tool bootstrap command: `make tools` if `.tools/bin/golangci-lint`, `.tools/bin/staticcheck`, or `.tools/bin/gosec` are missing.
- Baseline command: `make verify`
- Format command: `make fmt-check`
- Lint command: `make lint`
- Test command: `make test && make test-race`
- Additional gates: `make security && make coverage && make build && make smoke`
- Full gate command: `make verify`
- Dependency hygiene command for dependency-changing steps: `go mod tidy`

## Progress Tracking
- File: `PROGRESS.md`
- Requirement: Create `PROGRESS.md` before any quality-gate setup or implementation work begins. If it already exists, update it for this zerolog watch formatter plan before implementation begins.
- Update rule: After each step is completed, update `PROGRESS.md` with the completed step, validation results, commit reference if available, current status, and next step.

## Changelog Tracking
- File: `CHANGELOG.md`
- Standard: Keep a Changelog 1.0.0, <https://keepachangelog.com/en/1.0.0/>
- Requirement: Create `CHANGELOG.md` before any quality-gate setup or implementation work begins. If it already exists, keep its existing history and add entries for this plan under `## [Unreleased]`.
- Initial content: Include `# Changelog`, the standard preamble, and an `## [Unreleased]` section.
- Update rule: After each step is completed and validated, update `CHANGELOG.md` with human-readable notable changes under the appropriate `Unreleased` change-type headings before creating that step's commit.

## Incremental Steps

### Step 0: Progress and Changelog Tracking Setup
Goal: Create or refresh durable progress and changelog files for the zerolog watch formatter work before implementation starts.

Depends on:
- None

Changes:
- Update `PROGRESS.md` in the project root for this plan.
- Add the plan title/source, an ordered step checklist, current status, and an update log entry for starting the zerolog watch formatter plan.
- Preserve completed history from the prior MVP plan if useful, but make the current status point at this plan's next step.
- Confirm `CHANGELOG.md` exists in the project root and follows Keep a Changelog 1.0.0 structure.
- Add or preserve `# Changelog`, the standard preamble, and `## [Unreleased]`.
- Document that both files must be updated after each completed and validated step, before that step's commit.

Acceptance Criteria:
- `PROGRESS.md` identifies this zerolog watch formatter plan and includes every step in this plan.
- `CHANGELOG.md` remains valid Keep a Changelog content with `## [Unreleased]` at the top.

Validation:
- Confirm `PROGRESS.md` exists and contains this plan's checklist.
- Confirm `CHANGELOG.md` exists and follows the required Keep a Changelog 1.0.0 structure.
- Run `make fmt-check`.
- Run `make test`.

Progress:
- Mark Step 0 complete in `PROGRESS.md`, record validation results, set current status to Step 1, and identify Step 1 as next.

Changelog:
- Add an `Added` entry under `## [Unreleased]` for establishing progress tracking for the zerolog watch formatter work.

Commit:
- `docs: track zerolog watch formatter plan progress`

### Step 1: Add Zerolog Dependency
Goal: Introduce zerolog as the structured logging dependency without changing runtime behavior yet.

Depends on:
- Step 0

Changes:
- Update `go.mod` and `go.sum` by adding `github.com/rs/zerolog`.
- Run `go mod tidy` so module metadata is minimal and reproducible.
- Do not wire zerolog into command output in this step.

Acceptance Criteria:
- The module graph includes zerolog.
- Existing CLI behavior and tests remain unchanged.

Validation:
- Run `go mod tidy`.
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

Progress:
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Update `CHANGELOG.md` under `## [Unreleased]` with a human-readable dependency/setup entry after validation and before committing.

Commit:
- `build: add zerolog dependency`

### Step 2: Define Structured Watch Events
Goal: Create an internal event shape that represents every watch line before it is rendered.

Depends on:
- Step 1

Changes:
- Add a focused type near the current renderer, likely in `cmd/watch_render.go` unless tests show it belongs in a small new file:
  - `Provider string`
  - `Family codex.LogFamily`
  - `Category codex.LogCategory`
  - `Status string`
  - `Message string`
  - `Tokens int`
  - `ExitCode *int`
  - optional fields needed for future JSON output, such as `SourcePath`, `Reason`, `Tool`, or `Command`
- Refactor the existing renderer flow so `codex.ParseResult` is converted into `WatchEvent` values before any terminal formatting happens.
- Keep current grouping behavior:
  - command result follows matching tool call
  - `apply_patch` renders as `edit apply_patch`
  - token usage renders only after one or more pending actions
  - orphan token telemetry stays hidden
  - malformed JSON warnings produce warning events
- Add unit tests around event construction rather than only rendered strings.

Acceptance Criteria:
- Existing watch output behavior can be derived from `WatchEvent` values.
- Tests cover command success/failure, apply_patch edit, batch token usage, orphan token suppression, and malformed JSON warning events.
- No visible output format change is required yet.

Validation:
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

Progress:
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Update `CHANGELOG.md` under `## [Unreleased]` with a notable implementation entry after validation and before committing.

Commit:
- `feat: model codex watch output as structured events`

### Step 3: Render Watch Events With Zerolog ConsoleWriter
Goal: Replace the ad hoc watch formatter with a `zerolog.ConsoleWriter` renderer while preserving compact human-readable output.

Depends on:
- Step 2

Changes:
- Introduce a watch logger/formatter constructor, likely in `cmd/watch_render.go`, that accepts:
  - `io.Writer`
  - color mode or color-enabled boolean
  - any deterministic test options needed for output
- Use `zerolog.ConsoleWriter` with custom `FormatLevel`, `FormatMessage`, and `FormatPartValueByName` or equivalent supported hooks.
- Emit structured fields for at least:
  - `provider`
  - `family`
  - `category`
  - `status`
  - `message`
  - `tokens`
  - `exit_code`
- Format output toward the target shape:
  - `codex  watch   rollout-new.jsonl (cwd match)`
  - `codex  ok      run make verify (exit 0)`
  - `codex  tokens  213927 after run make verify`
  - `codex  edit    apply_patch`
  - `codex  fail    run go test ./... (exit 1)`
  - `codex  warn    malformed_json: bad record`
- Normalize existing watch warning output so inspection warnings and tail failures use the same structured renderer path as parser warnings.
- Keep truncation behavior for long commands/messages.
- Update existing watch renderer tests to assert deterministic no-color output.

Acceptance Criteria:
- `start --watch` still prints concise one-line events.
- The watch path uses zerolog for rendered watch lines instead of direct watch-specific `fmt.Fprintf` calls.
- Tests prove command results, token summaries, edit events, watch-file announcements, and warnings render correctly.

Validation:
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

Progress:
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Update `CHANGELOG.md` under `## [Unreleased]` with a notable formatter change after validation and before committing.

Commit:
- `feat: render codex watch events with zerolog`

### Step 4: Add Color Mode Flag and Color Policy
Goal: Add `--color auto|always|never` and apply it to the zerolog watch console renderer.

Depends on:
- Step 3

Changes:
- Add a persistent root flag in `cmd/root.go`:
  - `--color auto|always|never`
  - default `auto`
- Add validation for unsupported color values with an actionable error.
- Add a small color policy helper that resolves color mode to enabled/disabled for a specific output writer.
- In `auto`, enable colors only for terminal output.
- In `always`, force colors even in redirected output.
- In `never`, suppress ANSI color sequences.
- Apply color categories from the decision:
  - context/watch: cyan
  - conversation: white/bold
  - tool/run: blue
  - tool/edit: magenta
  - telemetry/tokens: yellow
  - success/ok: green
  - failure/fail: red
  - warning: yellow/bold
  - unknown: dim gray
- Add tests for flag registration, validation, and deterministic color output in `always`/`never` modes.

Acceptance Criteria:
- `agentreceipt --color never start --watch ...` emits no ANSI sequences.
- `agentreceipt --color always start --watch ...` emits ANSI color sequences for rendered watch events.
- Invalid values fail before watch starts.
- Existing non-watch commands are not materially changed beyond accepting the persistent flag.

Validation:
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

Progress:
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Update `CHANGELOG.md` under `## [Unreleased]` with a notable CLI flag entry after validation and before committing.

Commit:
- `feat: add color mode for watch output`

### Step 5: Update Docs and Smoke Coverage for Watch Formatting
Goal: Document the watch logger behavior and ensure the end-to-end smoke path covers the new flag surface.

Depends on:
- Step 4

Changes:
- Update `README.md` watch usage examples to include `--color auto|always|never`.
- Briefly document that watch output is human-readable by default and backed by structured events for future machine-readable rendering.
- Update `scripts/smoke.sh` if practical to exercise `start --watch --color never` in a deterministic way without making smoke tests flaky.
- If smoke coverage cannot reliably run watch mode because of timing or fixture constraints, document that limitation in `PROGRESS.md` and keep unit/integration coverage in `cmd/root_test.go`.

Acceptance Criteria:
- README users can discover `--color`.
- Smoke or command tests cover the new flag path.
- Documentation does not promise `--log-format` until it exists.

Validation:
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

Progress:
- Update `PROGRESS.md` with completion notes, validation results, commit reference if available, current status, and next step.

Changelog:
- Update `CHANGELOG.md` under `## [Unreleased]` with a documentation or test coverage entry after validation and before committing.

Commit:
- `docs: document structured watch logging color modes`

### Step 6: Final Review and Compatibility Pass
Goal: Verify the zerolog watch implementation is complete, scoped, and compatible with the existing receipt workflow.

Depends on:
- Step 5

Changes:
- Review the final diff for accidental broad logging rewrites outside the watch path.
- Confirm `go.mod` and `go.sum` contain only necessary dependency changes.
- Confirm watch warnings, watch-file announcements, command results, edit events, and token summaries all flow through the structured renderer.
- Confirm no tests rely on incidental ANSI behavior under `auto`.
- Confirm `PROGRESS.md` and `CHANGELOG.md` are current.

Acceptance Criteria:
- The feature is limited to the agreed first step: watch logger/formatter for `start --watch`.
- Future `--log-format console|json` can be added by reusing `WatchEvent` values without redesigning parsing.
- Full quality gates pass.

Validation:
- Run `go mod tidy`.
- Run `make fmt-check`.
- Run `make lint`.
- Run `make test`.
- Run `make test-race`.
- Run `make security`.
- Run `make coverage`.
- Run `make build`.
- Run `make smoke`.
- Run `make verify`.

Progress:
- Update `PROGRESS.md` with completion notes, validation results, final commit reference if available, current status as complete, and next step as "Plan complete".

Changelog:
- Update `CHANGELOG.md` under `## [Unreleased]` with any final notable cleanup after validation and before committing.

Commit:
- `chore: finalize zerolog watch formatter rollout`
