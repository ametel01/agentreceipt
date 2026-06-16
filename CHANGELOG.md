# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project will follow semantic versioning once releases begin.

## [Unreleased]

### Added

- Added initial project README, implementation plan, and Codex-first MVP reference specifications.
- Added durable progress tracking for the incremental implementation plan.
- Added Go module baseline, local quality-gate Makefile targets, lint configuration, CI matrix, and smoke-check script.
- Added Cobra CLI skeleton with the MVP command surface, command flags, version output, and command-discovery tests.
- Added Codex-first config defaults, receipt/session model contracts, deterministic JSON helpers, and canonical session storage layout helpers.
- Added append-only event log support with deterministic normalization, genesis hash chaining, replay verification, and JSONL persistence tests.
- Added Git monitor snapshot capture with start/final diff artifacts, patch hashes, git snapshot events, and final-diff mismatch detection.
- Added filesystem watcher support with debounced fsnotify events, changed-file summaries, sensitive path classification, and dependency file classification.
- Added session lifecycle orchestration for `start`, `status`, `live`, and `stop` with persisted state, active-session tracking, finalized manifests, event replay, and zero-Codex-evidence warnings.
- Added robust Codex JSONL parsing, provider trace export, `inspect codex`, active-session `import codex-jsonl`, parser warnings, command extraction, output redaction, and provider risk signals.
- Added confidence-aware review reports with risk reasons, command detection, capture confidence, evidence gaps, reviewer focus prompts, and JSON/Markdown/PR output modes.
