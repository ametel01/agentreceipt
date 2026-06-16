# AgentReceipt Technical Specification (Codex-first MVP)

## 1) Purpose

Define how to implement the AgentReceipt CLI so it records Codex-assisted sessions using a local sidecar model, produces cryptographically verifiable receipts from observable evidence, and generates reviewer-focused summaries for PR safety checks.

## 2) Language and tooling

Use **Go** for the MVP.

Recommended Go stack:

- `cobra` for command parsing
- `fsnotify` for filesystem watching
- `gopkg.in/yaml.v3` for config parsing
- `encoding/json` for event and receipt serialization
- `crypto/ed25519` for signing
- `encoding/hex` + `crypto/sha256` for hashing
- `regexp` for command/path redaction heuristics
- `github.com/google/uuid` for IDs (or time-based ULID helper)

Rationale:

- One static binary for the CLI
- Strong standard-library crypto and file/process primitives
- Good ecosystem for concurrent goroutines and event pipelines

### 2.1 Production-grade requirement (non-negotiable)

- Build and run as a production CLI from the first release, not a prototype.
- All failures are explicit and typed; no silent fallbacks in core verification paths.
- No command is considered done without:
  - structured logging with session IDs
  - non-leaky temporary file handling
  - deterministic file naming and atomic writes
  - input validation at trust boundaries (`path`, `command`, `provider payload`, `session ID`)
- Every artifact must be reproducible from source with a clean Git checkout.

## 3) Scope boundaries (MVP)

### In scope

- Explicit session lifecycle: `start` / `stop`.
- Event capture sources:
  - Git monitor
  - Filesystem watcher
  - Codex session log importer (best effort)
- Hash-chain event log and receipt signing
- Review outputs:
  - concise terminal summary
  - `--json`
  - `--md`
  - `--pr`
- PR workflow support through generated Markdown and a GitHub CLI convenience command.
- Codex provider research/diagnostic command for validating local log evidence before full capture.
- Policy-driven redaction for export/review payloads

### Out of scope (MVP)

- Auto-start on Codex execution
- Wrapped/managed agent execution
- Multi-agent comparison, trust scoring, cloud policy enforcement
- Hook-based Claude integration as primary path (deferred integration only)

## 4) Architecture

### 4.1 Process model

`agentreceipt start` launches a managed sidecar process (or a process tied to terminal lifecycle) that:

1. Creates a session record under global AgentReceipt storage keyed by repository path.
2. Starts background goroutines for:
   - Git snapshot monitor
   - Filesystem watcher
   - Optional Codex import watcher
3. Writes append-only events to `events.jsonl`.
4. Terminates on `agentreceipt stop`, computes final artifacts, signs receipt, and exits cleanly.

### 4.2 Event ingestion flow

```
fs events + git signals + codex logs
               |
               v
      Normalizer + confidence tags
               |
               v
      Append-only event log (JSONL)
               |
               v
   Diff snapshotter + risk scorer + review builder
               |
               v
      Receipt + signatures + summaries
```

## 5) Command behavior (Codex-first MVP)

### 5.1 Core commands

| Command | Responsibility |
| --- | --- |
| `agentreceipt init` | Bootstrap global AgentReceipt storage and keys under `~/.agentreceipt/` if missing; does not write repo-local files. |
| `agentreceipt install codex` | Detect Codex log directories and set parser preferences in config. |
| `agentreceipt install claude` | Deferred roadmap path; command may explain that Claude hook installation is not active in Codex-first MVP. |
| `agentreceipt start` | Fail-fast if git monitor or filesystem watcher cannot initialize. Create session, persist `manifest.json`, begin capture. |
| `agentreceipt status` | Show current session health and event summary counts. |
| `agentreceipt live` | Stream recent canonicalized events from current session. |
| `agentreceipt stop` | Finalize session, compute diff/hash/signature/manifest summary, emit warning if Codex provider evidence missing, exit success (non-blocking). |
| `agentreceipt review` | Produce concise summary in terminal (`--last`, `--session <id>`, `--security`, `--diff`, `--json`, `--md`, `--pr`, `--full`). |
| `agentreceipt verify` | Validate chain, manifest, diff hash, signature. |
| `agentreceipt export --json|--md|--pr` | Rehydrate finalized receipt in requested format. |
| `agentreceipt import codex-jsonl <path>` | Optional parser path for non-interactive Codex JSONL. |
| `agentreceipt inspect codex --last` | Provider research harness that reports Codex log discovery, parseability, candidate sessions, command/tool extraction coverage, and confidence. |
| `agentreceipt mark <message>` | Add a human context marker as a signed event in the active session. |
| `agentreceipt pr comment` | Generate PR Markdown and submit it with GitHub CLI when `gh` and a current PR are available. |

## 6) Data model

### 6.1 Optional explicit config

AgentReceipt uses Codex-first defaults without requiring a repo-local config file. Advanced users may pass an explicit config file with `--config`:

```yaml
version: 1
session:
  idle_timeout_minutes: 30
  auto_finalize_on_stop: true
capture:
  git: true
  filesystem: true
  claude_hooks: false
  codex_logs: true
  store_terminal_output: false
  store_provider_raw_logs: true
privacy:
  redact_secrets: true
  store_prompts: false
  store_raw_tool_outputs: false
  max_blob_bytes: 200000
review:
  require_tests_for_code_changes: true
  require_typecheck_for_ts: true
  flag_dependency_changes: true
  flag_auth_changes: true
  flag_secret_paths: true
sensitive_paths:
  - ".env"
  - ".env.*"
  - "*.pem"
  - "*.key"
  - "id_rsa"
  - "id_ed25519"
  - ".npmrc"
  - ".pypirc"
  - ".netrc"
  - ".aws/credentials"
  - ".github/workflows/**"
  - "src/auth/**"
  - "src/payments/**"
  - "src/security/**"
  - "src/crypto/**"
  - "migrations/**"
  - "Dockerfile"
  - "docker-compose.yml"
test_commands:
  - "npm test"
  - "npm run test"
  - "npm run lint"
  - "npm run typecheck"
  - "pnpm test"
  - "pnpm lint"
  - "pnpm typecheck"
  - "yarn test"
  - "cargo test"
  - "pytest"
  - "go test ./..."
  - "make test"
```

### 6.2 Event schema

```json
{
  "event_id": "evt_01J...",
  "session_id": "ar_ses_01J...",
  "seq": 12,
  "timestamp": "2026-06-16T12:00:00.000Z",
  "source": "git_monitor|fs_watcher|codex_session_log|manual_marker|receipt_finalizer",
  "type": "git.snapshot|fs.change|provider.command|provider.event|receipt.finalize",
  "provider": "codex|unknown",
  "cwd": "/repo",
  "payload": {},
  "prev_hash": "sha256:...",
  "event_hash": "sha256:..."
}
```

### 6.3 Receipt summary schema (abbreviated)

Key fields:

- `session_id`, `created_at`, `mode`, `agent.provider = codex`, `agent.provider_confidence = medium`
- `repo` (branch start/end, commit start/end, dirty flags)
- `summary`: changed files, detected commands, test/lint/typecheck detections, duration
- `capture_confidence`: `git_diff`, `filesystem_writes`, `provider_tool_events`, `file_reads`, `network_calls`
- `risk`: level + reasons
- `verification`: event chain hash, diff hash, signature info

## 7) Storage layout

```
~/.agentreceipt/
  repos/
    <repo-key>/
      sessions/
        ar_ses_<id>/
          events.jsonl
          receipt.json
          receipt.md
          review.md
          manifest.json
          diffs/
            000001.patch
            final.patch
          provider/
            codex/
              imported-session.jsonl
              parse-report.json
              traces/
                session-meta.ndjson
                timeline.ndjson
                tool-calls.ndjson
                command-events.ndjson
                errors.ndjson
                risk-signals.ndjson
                session-summary.ndjson
            claude/
              hook-events.jsonl
              transcript.jsonl
              parse-report.json
          blobs/
            sha256-...
          signatures/
            receipt.sig
```

Event log is append-only. Parsed Codex files should be treated as untrusted input and normalized with warnings.
Claude directories are reserved for roadmap compatibility and should not imply active Claude hook support in the Codex-first MVP.
Large provider payloads and command outputs should be stored as content-addressed local blobs when retained, with redacted references in exported reports.

## 8) Component design

### 8.1 Git monitor

- Capture at start:
  - repo root, branch, commit, dirty status, staged/unstaged diffs
- Ongoing:
  - snapshot after significant file-system bursts
  - snapshot on stop
  - `git diff` snapshots stored as diff patches under session `diffs/`
- Responsibility:
  - high-confidence evidence source

### 8.2 Filesystem watcher

- Use `fsnotify` with debounced event batching to avoid duplicate events.
- Persist create/modify/delete and rename where supported.
- Track changed files and sensitive-path flags.
- Responsibility:
  - high-confidence evidence source

### 8.3 Codex session-log importer

- Best-effort parser for known likely locations:
  - `$CODEX_HOME/sessions/`
  - `$CODEX_HOME/archived_sessions/`
  - `~/.codex/sessions/`
  - `~/.codex/archived_sessions/`
  - `~/.codex/session_index.jsonl`
- Parse line-delimited JSONL defensively.
- Never crash on malformed records.
- Preserve unknown event types as opaque `provider.event` payloads.
- Record parser warnings and confidence drops.
- Use supplemental local runtime sources only for low-confidence correlation:
  - `~/.codex/logs_2.sqlite`
  - `~/.codex/history.jsonl`
- Redact secret-like content before writing normalized trace outputs.
- Store trace extraction outputs under the active session's `provider/codex/traces/` directory.
- Responsibility:
  - optional/enrichment evidence

### 8.4 Event hash chain

- Each event computes `event_hash = sha256(prev_hash || canonical_event_json)`
- `prev_hash` of first event = seed value (`sha256("agentreceipt genesis")`)
- At stop, compute `event_chain_hash` as last event hash.

### 8.5 Receipt builder

- Build once at `stop`.
- Recompute final workspace diff and hash.
- Compare session file events vs final diff:
  - if mismatch, mark warning/critical risk
  - do not hard-fail by default

### 8.6 Signature module

- Generate/validate Ed25519 keypair in `~/.agentreceipt/keys/default.ed25519|default.pub`.
- Sign receipt summary + event chain hash + final diff hash + manifest hash.
- Persist detached signature.

### 8.7 Review generator

- Primary output concise terminal mode.
- `--json` outputs machine-parseable report.
- `--md` outputs short PR-style human summary.
- Include explicit confidence and missing-evidence sections.
- No hard-fail on missing provider events; only warning + downgraded confidence.

## 9) Session state machine

```
idle -> starting -> active -> finalizing -> finalized -> verified(optional)
```

Transitions:

- `start`: validate repo + watchers, create global session directories keyed by repo path, seed manifest
- `status/live`: readable only if active
- `stop`: snapshot final state -> build diffs -> compute hashes -> sign -> write artifacts
- On corruption, emit explicit warning and mark `receipt_verification=invalid` only for verify/report output.

## 10) Risk model and confidence

- Risk levels:
  - `info`
  - `low`
  - `medium`
  - `high`
  - `critical`
- Provider confidence:
  - Git diff = high
  - Filesystem writes = high
  - Codex logs = medium (best effort)
  - File reads = low-medium
  - Network calls = low
- If zero Codex provider events observed:
  - continue and mark provider confidence downgrade + warning
- Default risk signals must cover:
  - secret/credential path changes
  - auth/payment/security/crypto/infra path changes
  - dependency and lockfile changes
  - destructive shell commands
  - external network commands
  - package publish/install commands
  - missing tests/lint/typecheck after code changes
  - final diff mismatch
  - receipt verification failure

## 11) Error handling policy

- `start`:
  - hard-fail if git monitor + fs watcher unavailable (explicitly matches decision)
- `stop`:
  - never fail due to missing Codex log evidence
  - record warnings for missing/unparseable provider logs
- `verify`:
  - explicit invalid status when chain/signature/hash mismatch
  - non-zero exit code for scriptability

## 12) Security and redaction

- Redact values matching known secret/token patterns in review/export.
- Keep raw logs locally by default, excluded from exported review text by default.
- Cap large blobs and replace with hashes when writing payload snapshots.

## 13) Test plan (targeted MVP)

1. Start/stop lifecycle with valid repo (positive path).
2. Start fails when git unavailable / path not repo (negative path).
3. FS events captured and reflected in final changed-file list.
4. Git snapshot hash reproducibility under stable file state.
5. Codex importer parses malformed JSONL robustly.
6. Finalize with zero Codex events → receipt generated + warning, non-fatal.
7. Final diff mismatch sets risk warning and verify invalid as expected.
8. `inspect codex --last` reports log discovery and parser confidence without requiring an active session.
9. `review --pr` and `pr comment` produce concise PR Markdown without raw prompts or tool outputs.
10. `mark` persists human context as a chained event in the active session.

## 14) Strict quality gates

No feature is complete unless all gates pass.

### 14.1 Required local checks

Run in order for every developer before merge:

```bash
gofmt -s -w .
test -z "$(gofmt -s -l .)"
go test ./...
go test -race ./...
go vet ./...
staticcheck ./...
golangci-lint run ./...
gosec ./...
go test ./... -run Test -count=1 -coverprofile=coverage.out
go tool cover -func=coverage.out | awk '/total:/ { if ($3+0 < 80.0) exit 1 }'
```

All must pass; no exception for Codex-only edge paths.

### 14.2 CI gates

GitHub Actions pipeline must enforce:

- matrix: Linux + macOS
- deterministic build (`go build ./...`)
- all local checks from 14.1
- CLI smoke tests:
  - `agentreceipt init`
  - `agentreceipt start`/`stop` with mocked event sources
  - `agentreceipt verify` on a generated fixture receipt
- signature verification smoke:
  - key exists (or generated deterministically) and receipt verification returns valid
- artifact checks:
  - no uncommitted generated session artifacts in repository root from tests

### 14.3 Quality gate policy

- Any failing gate blocks release and PR merge.
- Failing static analysis or security checks require explicit ticket + owner before bypass.
- Coverage/coverage gates are minimum 80% for packages in `internal/` and `cmd/`.

## 15) Implementation sequencing

1. Define schemas + config + session manifest.
2. Implement storage and hash-chain/event logger.
3. Implement git + fs capture and lifecycle commands.
4. Add Codex importer and confidence annotations.
5. Implement receipt/review/verify/export outputs.
6. Add key management/signing and end-to-end smoke checks.
7. Polish UX and documentation.

## 16) Open questions

- Which Cobra/Viper variants to standardize across commands?
- Should signature key naming support per-project keys later (current spec assumes one local default key)?
- Whether global `~/.agentreceipt/repos/*/sessions` retention is time-based or manual (MVP: manual).

## 17) Recommended implementation baseline

Go is a good fit for this exact requirement and should be the implementation language for MVP.
