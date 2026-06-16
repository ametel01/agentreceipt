# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project will follow semantic versioning once releases begin.

## [Unreleased]

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
