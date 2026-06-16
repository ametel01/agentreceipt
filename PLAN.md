# Implementation Plan

## Source Documents
- Path: `/Users/alexmetelli/source/agentreceipt/docs/PRD.md`
  - Role: Primary PRD
  - Summary: Defines AgentReceipt’s MVP scope (Codex-first, local-only, sidecar evidence capture, receipt signing, review/verify/export), supported/unsupported workflows, commands, storage layout, and core safety/non-scoring philosophy.
- Path: `/Users/alexmetelli/source/agentreceipt/docs/TECH_SPEC.md`
  - Role: Technical design and architecture
  - Summary: Mandates Go/Cobra/ed25519 stack, session model, event hash chain, storage layout, command behavior, risk/confidence model, deterministic artifact generation, and strict quality gates for release.
- Path: `/Users/alexmetelli/source/agentreceipt/docs/CODEX_TRACE_REPORT_SPEC.md`
  - Role: Codex ingestion and evidence schema support
  - Summary: Specifies Codex session/import parsing schema expectations, risk signal formats, timeline/tool-call/command/error schemas, evidence confidence model, and reproducibility artifacts required for report publishing.

## Goals
- Deliver a production-grade Go CLI `agentreceipt` with `start`/`stop` sidecar session capture, receipt generation, verification, and review/export surfaces.
- Provide explicit, local evidence for Codex-assisted changes using Git + filesystem as high-confidence sources and Codex log import as best-effort enrichment.
- Support reviewer-facing outputs (`review`, `verify`, `export`) with clear confidence labels, risk signals, and missing-evidence gaps.
- Support PR workflow outputs through `review --pr`, `export --pr`, and `pr comment` via GitHub CLI.
- Include a Codex provider research path (`inspect codex --last`) so log availability and parser confidence can be validated before full capture work.
- Maintain no-wrapper execution: agent tools run normally; AgentReceipt observes sideband.
- Ensure deterministic, reproducible, auditable artifacts under `.agentreceipt/sessions/<session_id>/`.

## Non-Goals
- Claude hook-first workflow and Claude primary integration (deferred).
- Wrapped/managed execution (`agentreceipt run -- codex`) and agent orchestration.
- Agent scoring engines, trust profiles, or recommendation logic.
- Hosted/cloud SaaS mode, prompt uploads, or external telemetry pipeline.
- Hosted GitHub App checks; only local/CLI-generated PR comments are in scope.
- Full OS-level tracing (eBPF/dtrace), network MITM, or dependency on MCP proxies.

## Assumptions and Open Questions
- Assumption: Codex remains the MVP primary provider; Claude support can be treated as roadmap-only until Codex path is stable.
- Assumption: Default signing key remains single local key (`~/.agentreceipt/keys/default.ed25519`) for MVP unless project policy requires key-per-project later.
- Assumption: CI and developer machines can install/use `staticcheck`, `golangci-lint`, and `gosec`; if unavailable, plan falls back to explicit installation pre-step.
- Open question: retention policy for `.agentreceipt/sessions` in MVP (manual cleanup vs time-based). Conservative choice: manual cleanup with explicit docs, matching Tech Spec.
- Open question: whether sidecar session runs in foreground with terminal-bound lifecycle or via explicit start/stop process management; conservative choice: start/stop process with a persisted session PID/state for reliability.
- Open question: exact command allowlist for test/lint/typecheck detection per language ecosystem; conservative choice: start with documented defaults and allow policy overrides.
- Open question: required minimum coverage threshold enforcement scope (`./...` or project packages only); conservative choice: keep 80% target for all packages, initially.
- Open question: whether `agentreceipt pr comment` should require an existing checked-out branch with a current PR or accept an explicit PR number; conservative choice: support current-branch PR first and add explicit PR targeting later.

## Quality Gates
- Setup status: No existing toolchain config detected in repository root; quality gates must be added before implementation work.
- Baseline command: `go test ./...` (after initial Go scaffold is created; expected to pass on clean baseline).
- Format command: `test -z "$(gofmt -s -l .)"`.
- Lint command: `golangci-lint run ./... && staticcheck ./... && go vet ./... && gosec ./...`.
- Test command: `go test ./... && go test -race ./...`.
- Additional gates: `go test ./... -run Test -count=1 -coverprofile=coverage.out && go tool cover -func=coverage.out | awk '/total:/ { if ($3+0 < 80.0) exit 1 }'`.
- Build command: `go build ./...`.

## Progress Tracking
- File: `PROGRESS.md`
- Requirement: Create `PROGRESS.md` before any quality-gate setup or implementation work begins.
- Update rule: After each step is completed, update `PROGRESS.md` with completed step, validation results, commit ref (if available), current status, and next step.

## Incremental Steps

### Step 0: Progress Tracking Setup
Goal: Create a durable progress log the user can consult while the plan is being executed.

Depends on: none

Changes:
- Create `PROGRESS.md` with:
  - plan title and source documents
  - ordered step checklist
  - current status marker
  - short update log table (`date`, `step`, `status`, `validation`, `commit`, `notes`)
  - explicit rule that `PROGRESS.md` must be updated after every completed step

Validation:
- Confirm `PROGRESS.md` exists.
- Confirm it contains the step checklist and update format.

Progress:
- Mark Step 0 complete in `PROGRESS.md`, record validation and next step.

Commit:
- `docs: add progress tracking scaffold`

### Step 1: Quality Gates Setup
Goal: Make format/lint/test/build checks explicit and runnable before feature work.

Depends on:
- Step 0

Changes:
- Add `go.mod` and baseline module config for Go CLI project.
- Add `Makefile` with the following targets:
  - `fmt`, `fmt-check`, `lint`, `test`, `test-race`, `build`, `verify`, `security`, `coverage`.
- Add lint/security configs:
  - `.golangci.yml`
  - optional `gosec`/`staticcheck` config files if needed
- Add CI workflow `.github/workflows/ci.yml` (Linux + macOS matrix) including required checks and CLI smoke tests.
- Add `scripts/` helper targets for reproducible fixtures and smoke checks as needed.

Validation:
- Run setup gates exactly as in `## Quality Gates`:
  - `test -z "$(gofmt -s -l .)"`
  - `golangci-lint run ./...`
  - `staticcheck ./...`
  - `go vet ./...`
  - `go test ./...`
  - `go test -race ./...`
  - `gosec ./...`
  - `go test ./... -run Test -count=1 -coverprofile=coverage.out`
  - `go tool cover -func=coverage.out | awk '/total:/ { if ($3+0 < 80.0) exit 1 }'`
  - `go build ./...`

Progress:
- Mark Step 1 complete in `PROGRESS.md` with gate outcomes and next step.

Commit:
- `ci: add go quality gate pipeline and local make targets`

### Step 2: Bootstrap CLI Entry + Command Skeleton
Goal: Create a runnable Cobra CLI with all top-level commands discovered in PRD/Tech Spec as stubs.

Depends on:
- Step 0
- Step 1

Changes:
- Add `main.go` and `cmd/root.go` command registration.
- Implement command set with stub handlers and consistent UX:
  - `init`, `install codex`, `install claude`, `start`, `status`, `live`, `stop`, `review`, `verify`, `export`, `import codex-jsonl`, `inspect codex`, `mark`, `pr comment`.
- Add command doc/help text matching PRD terms and expected defaults.
- Add `cmd/version` behavior and exit codes for scriptability.
- Create initial command-focused tests for root help and command discovery.

Acceptance Criteria:
- Running `agentreceipt` shows available commands and global flags without panic.
- `--help` output includes all primary commands.
- Deferred commands (`install claude`) explain their roadmap status without configuring runtime hooks.

Validation:
- Run all quality gates from section above.

Progress:
- Update `PROGRESS.md` with validation results and next step.

Commit:
- `feat: scaffold cobra CLI and command surface`

### Step 3: Define Config, Session, and Storage Contracts
Goal: Encode canonical schemas and directories used by every command and artifact.

Depends on:
- Step 2

Changes:
- Add `internal/config`:
  - `.agentreceipt.yml` schema parsing and write helpers.
  - policy defaults from Tech Spec (sensitive paths, review checks, test command list).
- Add `internal/model` with event, receipt, manifest, review, and review-report types.
- Add `internal/storage` helpers for `.agentreceipt/sessions/<session_id>/` layout, manifest, and artifact naming.
- Include `.agentreceipt/policy.yml`, `provider/codex/traces/`, reserved `provider/claude/`, `blobs/`, `diffs/`, and `signatures/` path constants.
- Add schema/version constants and forward-compatible deserialization behavior.
- Include config validation with actionable errors.

Acceptance Criteria:
- Config can be serialized/deserialized and merged with defaults.
- Receipt/session models support JSON + deterministic marshaling.

Validation:
- Run all quality gates from section above.

Progress:
- Update `PROGRESS.md` with model/schema completion, validation results, and next step.

Commit:
- `feat: add config and event/receipt schema model`

### Step 4: Implement Event Log and Hash Chain
Goal: Persist append-only events and compute verifiable chain/hash metadata.

Depends on:
- Step 3

Changes:
- Add event normalizer and canonical JSON serialization.
- Implement hash-chain functions:
  - genesis seed
  - `event_hash = sha256(prev_hash || event_json)`
  - chain hash extraction at finalize.
- Add immutable `events.jsonl` writer and atomic append semantics.
- Add tests for deterministic hash ordering and chain continuity.

Acceptance Criteria:
- Events are written in strict sequence with immutable previous hash linkage.
- Replaying saved events reproduces identical event hashes.

Validation:
- Run all quality gates from section above.

Progress:
- Update `PROGRESS.md` with continuity checks and next step.

Commit:
- `feat: add event log with deterministic hash chaining`

### Step 5: Git Monitor and Snapshot Capture
Goal: Add high-confidence git source with start/end state and periodic snapshots.

Depends on:
- Step 4

Changes:
- Add `internal/capture/gitmonitor` for:
  - session start: toplevel, branch, HEAD, status, staged/unstaged diff snapshots
  - snapshot logic and debounced updates
  - final snapshot/diff hash generation
  - diff patch persistence under `diffs/`
- Add integration tests with temp git repo fixture to ensure capture resilience.

Acceptance Criteria:
- Start emits start-state event set.
- Stop persists final diff and final hash.
- Final diff mismatch detection is detectable when external changes occur after session end.

Validation:
- Run all quality gates from section above.

Progress:
- Update `PROGRESS.md` with git capture validation and next step.

Commit:
- `feat: add git monitor snapshots and diff artifacts`

### Step 6: Filesystem Watcher and Change Classification
Goal: Record create/modify/delete events and classify risky paths in near-real time.

Depends on:
- Step 5

Changes:
- Add `internal/capture/fswatcher` using `fsnotify` with debounce and burst coalescing.
- Emit events for write/delete/create/rename where supported.
- Track changed file list, dependency file changes, and sensitive path matches from policy.
- Persist watcher events into chain via normalizer.

Acceptance Criteria:
- Changing tracked files creates canonical fs events and reflects in session changed-files summary.
- Sensitive/dependency flags are attached when paths match policy.

Validation:
- Run all quality gates from section above.

Progress:
- Update `PROGRESS.md` with watcher and classifier outcomes and next step.

Commit:
- `feat: implement filesystem watcher and sensitivity classification`

### Step 7: Sidecar Lifecycle Orchestration
Goal: Wire `start`, `status`, `live`, `stop` commands to controlled session process/state.

Depends on:
- Step 6

Changes:
- Add session state manager and PID/state persistence under `.agentreceipt/sessions/<session_id>/`.
- `start`: initialize session, spawn capture goroutines, write manifest.
- `status`: show active state, event counts, risk summary, capture source status.
- `live`: stream canonicalized recent events.
- `stop`: stop watchers cleanly, finalize manifests, call receipt builder.
- Ensure idempotent and safe cleanup behavior.

Acceptance Criteria:
- `start` creates an active session and `status` reflects it.
- `stop` finalizes artifacts and leaves a valid session final state.
- No hard failure when zero Codex events are present.

Validation:
- Run all quality gates from section above.

Progress:
- Update `PROGRESS.md` with lifecycle behavior validation and next step.

Commit:
- `feat: implement session lifecycle sidecar orchestration`

### Step 8: Codex Log Ingestion and Best-Effort Provider Events
Goal: Add tolerant Codex importer with explicit confidence and warning handling.

Depends on:
- Step 7

Changes:
- Add `internal/provider/codex` parser for candidate paths and `codex-run` style JSONL imports.
- Add `inspect codex --last` research harness for log discovery, parser confidence, candidate session metadata, tool/command extraction coverage, and parse warnings.
- Implement defensive parsing:
  - stream parse line-by-line
  - unknown event passthrough into `provider.event`
  - parse warnings instead of hard failures
  - output size cap and command/result redaction
- Add timeline/tool-call/command/error/risk signal extraction into canonical events.
- Write provider trace outputs under `.agentreceipt/sessions/<session_id>/provider/codex/traces/` when tied to an active/finalized session.
- Treat `logs_2.sqlite` and `history.jsonl` as supplemental low-confidence correlation inputs only.
- Add `import codex-jsonl` command path and optional auto-watch in session lifecycle.

Acceptance Criteria:
- Malformed JSONL records never crash import.
- Missing/incomplete fields generate warning events with lowered confidence.
- Commands/outputs are captured with structured metadata when available.
- Missing Codex logs create an explicit gap and never block receipt finalization.

Validation:
- Run all quality gates from section above.

Progress:
- Update `PROGRESS.md` with parsing coverage and confidence behavior, then note next step.

Commit:
- `feat: add robust Codex JSONL importer and provider event normalization`

### Step 9: Risk Engine, Confidence Model, and Evidence Reporting
Goal: Compute and expose risk, missing-evidence, and confidence outputs used in review.

Depends on:
- Step 8

Changes:
- Add deterministic rule engine for:
  - sensitive paths, auth/payment/security/infra paths
  - dependency changes
  - detected test/lint/typecheck commands
  - command-risk regexes from spec
- Implement capture confidence per source (`high/medium/low`) and explicit downgrade markers.
- Add review data assembly with explicit warnings, reviewer focus points, and "gaps" section.
- Add review modes required by PRD: `--last`, `--session <id>`, `--security`, `--diff`, `--json`, `--md`, and `--pr`.

Acceptance Criteria:
- Review output contains risk level, reasons, command detection results, and capture confidence table.
- No hard-fail behavior on low-confidence evidence; warnings are explicit.

Validation:
- Run all quality gates from section above.

Progress:
- Update `PROGRESS.md` with risk model and report section verification, then next step.

Commit:
- `feat: add risk assessment and confidence-aware review data`

### Step 10: Receipt Build, Sign, Verify, and Export Surfaces
Goal: Make final artifacts complete, signed, and verifiable.

Depends on:
- Step 9

Changes:
- Add signature module using Ed25519 key management (`~/.agentreceipt/keys/default.ed25519|default.pub`).
- Implement `agentreceipt stop` finalization writes:
  - `receipt.json`, `receipt.md`, `review.md`, `manifest.json`, `signatures/receipt.sig`, final patch
- Implement `verify` checks:
  - event chain, manifest, final diff hash, signature validity
- Implement `review --json|--md|--pr` and `export --json|--md|--pr`.
- Implement mismatch handling between final diff and recorded event chain as non-fatal warning by default.

Acceptance Criteria:
- Generated receipts verify valid after finalization.
- Mismatch and missing-provider-evidence paths are reported with downgraded confidence, not hard-fail.
- `review --pr` output is PR-consumable and includes explicit focus/warnings.

Validation:
- Run all quality gates from section above.

Progress:
- Update `PROGRESS.md` with artifact/signature verification status and next step.

Commit:
- `feat: implement receipt signing, verify, and export/review output formats`

### Step 11: Manual Marker Command and Policy Controls
Goal: Add explicit human context hooks and operational controls.

Depends on:
- Step 10

Changes:
- Add `mark` command to add signed context events.
- Add optional output redaction and retention policy behavior consistent with PRD/privacy constraints.
- Add `install claude` command scaffold as roadmap-compatible placeholder (documented deferred), without enabling non-MVP runtime behavior.
- Add `pr comment` command that shells out to `gh pr comment --body-file <generated markdown>` after generating local PR Markdown.
- Expose `.agentreceipt.yml` update behavior and validation.

Acceptance Criteria:
- `mark` writes signed context into session events.
- No hidden network/file upload behaviors are introduced.
- `install claude` remains non-MVP/no-op with clear user messaging.
- `pr comment` fails clearly when `gh` is unavailable, no PR is detected, or GitHub CLI exits non-zero.

Validation:
- Run all quality gates from section above.

Progress:
- Update `PROGRESS.md` with control feature completion and next step.

Commit:
- `feat: add manual marker and deferred provider install controls`

### Step 12: Fixtures, Integration Tests, and CI Hardening
Goal: Lock critical behavior with test evidence and release-ready CI.

Depends on:
- Step 11

Changes:
- Add fixture-driven tests for:
  - start/stop lifecycle
  - fs watcher event capture
  - codex import malformed/partial logs
  - zero-provider-events warning path
  - inspect codex missing-log and parse-warning paths
  - PR Markdown generation without raw prompts/tool outputs
- Add end-to-end smoke harness for:
  - init/start/stop
  - verify on generated receipt
  - signature generation validation
  - review/export PR Markdown generation
- Finalize docs (`README`, install/usage examples, command flags).
- Ensure no tracked session artifacts in repository after tests and CI.

Acceptance Criteria:
- CI matrix Linux+macOS executes all required quality gates and passes locally reproducibly.
- Smoke tests cover explicit MVP success/failure paths in docs.

Validation:
- Run all quality gates from section above after test additions.

Progress:
- Update `PROGRESS.md` with test/CI status and final step note.

Commit:
- `test: add MVP fixtures and tighten CI verification checks`
