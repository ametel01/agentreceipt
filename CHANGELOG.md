# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project follows semantic versioning.

## [Unreleased]

## [0.4.2] - 2026-06-17

### Fixed

- Rejected unknown top-level receipt JSON fields during local and bundle verification so unsigned receipt content cannot pass as authenticated.
- Recorded the actual detected provider label in signed receipts instead of hard-coding Codex.
- Recognized `make verify` as default test evidence in review command detection.
- Detected review git bases from configured upstreams, `origin/HEAD`, and non-main default branch names such as `trunk` and `develop`.
- Limited missing-test review prompts to sessions with code file changes, avoiding docs-only noise.

## [0.4.1] - 2026-06-17

### Changed

- Improved the curl installer output with a clear `AGENT RECEIPT` ASCII banner and step-by-step progress bar while preserving checksum failure diagnostics.

## [0.4.0] - 2026-06-17

### Added

- Added Claude hook MVP support with dry-run hook installation, guarded settings merges, active-session hook ingestion, and provider-neutral review confidence.

### Fixed

- Serialized AgentReceipt event-log appenders so concurrent provider, marker, and filesystem watcher writes preserve a replayable hash chain.
- Enforced Codex provider privacy defaults so normalized events omit prompt text and raw tool output unless config explicitly opts in.
- Carried Codex provider risk signals into final review and receipt risk reasons.
- Applied explicit review config to quality-command detection and dependency, auth, secret-path, test, and typecheck policy decisions.
- Validated filesystem watcher identity before the stop fallback signals a recorded PID.
- Implemented read-only `install codex` detection for local Codex log availability.
- Added `verify bundle` for local CI-style verification of portable AgentReceipt artifact bundles.

## [0.3.0] - 2026-06-17

### Added

- Added a Claude provider design covering hook event normalization, storage and privacy behavior, install command requirements, and MVP acceptance criteria.
- Added a GitHub PR workflow design covering local-only PR comments, future CI-assisted receipt checks, artifact contracts, and deterministic policy boundaries.

### Fixed

- Fixed session filesystem capture so `agentreceipt start` launches a durable watcher sidecar, records `fs.change` events while active, and flushes watcher evidence before `stop` finalizes the receipt.
- Fixed review summaries so Codex command results update detected command status to `success` or `failed` when matching result evidence is present, while attempt-only commands remain `unknown`.
- Fixed review flag behavior by making `review --codex-jsonl` import a Codex trace into the active session before review and removing inactive `--full` and `--provider` flags.
- Fixed receipt verification portability by embedding the signer public key and key ID in new receipts while preserving legacy local-key verification.
- Fixed Codex watch tailing so large appended logs are read in bounded chunks while preserving complete-line offsets and partial-line safety.
- Fixed confidence reporting so Codex parse-warning-only evidence does not count as imported provider tool evidence.

## [0.2.0] - 2026-06-17

### Added

- Added high/medium/low command-risk classification for Codex tool calls, with live `start --watch` risk badges that preserve command outcome status and add a detail line for high-risk commands.
- Added `start --watch` resume behavior so rerunning it after Ctrl-C attaches to the active session instead of failing with a concurrent-session error.
- Added prominent README limitations and Apache-2.0 licensing for the release.

### Fixed

- Fixed the first token summary after resuming `start --watch` so it reports the delta from the last imported Codex token total instead of repeating the cumulative session total.
- Fixed `secret_access` command-risk false positives for commit messages and other prose that mention words like "token" without reading credential material.

## [0.1.0] - 2026-06-17

### Added

- Added initial project README, implementation plan, and Codex-first MVP reference specifications.
- Added durable progress tracking for the incremental implementation plan.
- Added progress and changelog tracking for the zerolog watch formatter rollout.
- Added `github.com/rs/zerolog` as the structured logging dependency for the watch formatter rollout.
- Added structured `WatchEvent` modeling for Codex watch command results, edits, token summaries, and parser warnings.
- Added zerolog `ConsoleWriter` rendering for compact Codex watch events and normalized watch warnings.
- Added `--color auto|always|never` for watch output with deterministic forced-color and no-color behavior.
- Added smoke coverage and README guidance for structured watch output and color modes.
- Added per-action token delta display in watch output, with the session token total shown on the same line.
- Added `scripts/extract-release-notes.sh` for CI release jobs to extract GitHub Release notes for `Unreleased` or a specific SemVer section from `CHANGELOG.md`.
- Added tag-driven GitHub Release workflow with verified builds, changelog-derived release notes, cross-platform archives, and checksums.
- Added curl-based installer for Linux and macOS release archives.
- Added Go module baseline, local quality-gate Makefile targets, lint configuration, CI matrix, and smoke-check script.
- Added Cobra CLI skeleton with the MVP command surface, command flags, version output, and command-discovery tests.
- Added Codex-first config defaults, receipt/session model contracts, deterministic JSON helpers, and canonical session storage layout helpers.
- Added append-only event log support with deterministic normalization, genesis hash chaining, replay verification, and JSONL persistence tests.
- Added Git monitor snapshot capture with start/final diff artifacts, patch hashes, git snapshot events, and final-diff mismatch detection.
- Added filesystem watcher support with debounced fsnotify events, changed-file summaries, sensitive path classification, and dependency file classification.
- Added session lifecycle orchestration for `start`, `status`, `live`, and `stop` with persisted state, active-session tracking, finalized manifests, event replay, and zero-Codex-evidence warnings.
- Added robust Codex JSONL parsing, provider trace export, `inspect codex`, active-session `import codex-jsonl`, parser warnings, command extraction, output redaction, and provider risk signals.
- Added foreground `start --watch` support for live Codex JSONL tailing, repo-aware session matching, real-time tool/command output, and automatic provider-event import into active receipts.
- Added typed Codex log categories and families for conversation, tool, telemetry, and context records, used them to render compact one-line `start --watch` action completions with only per-action or per-batch token totals, suppressed unpaired token telemetry, and defaulted watch selection to the newest matching Codex log instead of every historical repo log.
- Added confidence-aware review reports with risk reasons, command detection, capture confidence, evidence gaps, reviewer focus prompts, and JSON/Markdown/PR output modes.
- Added signed receipt finalization with Ed25519 key management, receipt/review artifact writing, integrity verification, and `export --json|--md|--pr` output.
- Added repository initialization, signed manual marker events, policy defaults, and guarded GitHub CLI PR comment posting.
- Added end-to-end MVP smoke coverage for init/start/import/mark/stop/verify/review/export and refreshed README usage examples.
- Changed session storage to global AgentReceipt home storage so repositories are not polluted with `.agentreceipt` directories or repo-local config files.

### Changed

- Refocused README positioning around the live `start --watch` session view as the primary workflow.
- Changed `agentreceipt review` to lead with Git-derived branch state, ahead/behind counts, branch/workspace diff stats, working-tree counts, and receipt/current-workspace diff agreement instead of generic capture-confidence labels, with color-coded terminal state when color output is enabled.
- Clarified that `zerolog` is reserved for structured streaming runtime events while review, receipt, verify, Markdown, PR, and short command responses stay on explicit renderers.
