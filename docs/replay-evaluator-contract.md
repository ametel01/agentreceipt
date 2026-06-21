# Replay Evaluator Contract

`agentreceipt replay` builds a factual, machine-readable view of one finalized session from stored
artifacts. It does not rerun commands, reapply patches, call models, or apply policy scoring.

The new `agentreceipt focus --json` command consumes the same `replay.json` payload to emit a compact
`agentreceipt.session_focus` report for reviewer-agent loops. This focused payload is a projection of
replay evidence (not a replacement for `replay`), with deterministic `verdict`, `top_reasons`,
`review_tasks`, `changed_files`, `failed_gates`, and `evidence_refs`.

`instruction_files` from replay is forwarded to focus output so loop callers can reconcile
start-up policy files without re-parsing events.

`changed_files` entries are per-file dossiers with:

- `path`
- `action`
- `category`
- `sensitive`
- `dependency`
- `symbols`
- `read_before_edit` (`pass`, `fail`, `warn`, `unknown`, or `not_applicable`)
- `related_context_read` (`pass`, `fail`, `warn`, `unknown`, or `not_applicable`)
- `tests_related` (explicitly matched test commands)
- `commands_touching_file` (explicitly matched commands)
- `review_reasons`
- `evidence_refs`

`review_tasks` entries are structured and ranked with:

- `id`
- `priority` (`P0`, `P1`, `P2`, `P3`)
- `kind`
- `question`
- `paths`
- `symbols`
- `evidence_refs`
- `confidence`
- `source`

Task kind examples include:

- `integrity_failure`
- `authenticity_unverifiable`
- `failed_gate`
- `failed_command`
- `risky_file`
- `missing_test`
- `diff_mismatch`
- `dependency_change`
- `sensitive_change`
- `generated_change`
- `evidence_gap`

## Report Shape

The top-level `replay.json` payload is additive and keeps the existing verifier fields while adding
evaluator-facing sections:

- `verification`
- `evaluator_signals`
- `instruction_files`
- `quality_gates`
- `patch_summary`
- `workspace_change_summary`
- `policy_checks`
- `review_focus`
- `privacy`
- `claims`
- `outcome`
- `commands`
- `files`
- `gaps`
- `artifacts`

`instruction_files` includes metadata captured from `AGENTS.md` and `CLAUDE.md` at session start:

- `path`
- `hash`
- `size`
- `mtime`
- `summary`

The existing `valid`, `signature_valid`, `signature_error`, `signature_error_code`, and `signed_by`
fields remain available for compatibility.

## Verification Semantics

Verification separates integrity from authenticity and trust:

- `integrity_valid` reports whether the event chain, final patch, manifest, and receipt hashes all
  validate.
- `authenticity_valid` reports whether the embedded signer material verified successfully.
- `authenticity_status` is one of `authenticated`, `unverifiable`, or `failed`.
- `trust_status` is one of `trusted`, `not_trusted`, `not_configured`, or `policy_invalid`.
- `signer_trusted` reports the trust-policy result.
- `policy_valid` reports whether the trust policy itself was valid.
- `overall_verdict` and `overall_reason` summarize the combined result.

Legacy receipts that lack embedded signer material are reported as integrity-valid when their hashes
match, but authenticity is `unverifiable`. A `signature_valid=false` result does not by itself mean
that the receipt artifacts were tampered with.

## Trust Policy Inputs

Trust policy is local and deterministic.

- `trust.trusted_signer_key_ids` in config supplies trusted key IDs.
- `agentreceipt replay --trusted-signer-key-id <id>` adds trusted IDs on the command line.

The replay report never relies on a network trust service.

## Quality Gates

`quality_gates` summarizes command-classified checks:

- `format`
- `lint`
- `tests`
- `race_tests`
- `typecheck`
- `security`
- `coverage`
- `build`
- `smoke`
- `verify`

Each gate reports:

- `status`
- `commands`
- `evidence_refs`
- `last_exit_code`
- `confidence`

Statuses are `passed`, `failed`, `not_run`, or `unknown`.

## Patch Summary

`patch_summary` provides a semantic view of `diffs/final.patch` without exposing raw hunk bodies.

It includes:

- file counts by category: `production`, `test`, `docs`, `config`, `dependency`,
  `generated_or_unknown`
- `additions` and `deletions`
- semantic `changed_files` entries with path, action, category, sensitive/dependency flags, symbols,
  and evidence refs
- `changed_go_symbols` where simple and reliable symbol hints can be extracted
- `tests_changed`
- `production_changed_without_tests_changed`

The existing `files` array remains available for direct file lookup.

`workspace_change_summary` separates workspace state at session start from changes introduced during the session:

- `pre_existing_dirty_files`
- `agent_touched_pre_existing_files`
- `agent_created_changes`
- `agent_modified_clean_files`
- `final_diff_matches_workspace`
- `final_diff_matches_branch`

The pre-existing file lists are review context and are not automatic blockers on their own.

## Policy Checks And Review Focus

`policy_checks` is a factual policy surface, not a score.

Current checks include:

- `target_file_read_before_edit`
- `related_context_read_before_edit`
- `tests_run_after_code_changes`
- `lint_run_after_code_changes`
- `typecheck_run_when_applicable`
- `destructive_command_used`
- `network_command_used`
- `dependency_file_changed`
- `sensitive_file_changed`
- `ci_or_security_file_changed`
- `generated_file_changed`
- `commit_created`

Each check reports:

- `status`
- `message`
- `confidence`
- `evidence_refs`

Statuses are `pass`, `fail`, `warn`, `not_applicable`, or `unknown`.

`review_focus` turns the same evidence into bounded prompts for human or automated review. It stays
factual and is not a scoring system.

## Privacy And Claims

`privacy` describes how the replay export avoids leaking sensitive content:

- `redaction_applied`
- `redacted_fields`
- `redaction_patterns`
- `output_caps`
- `sensitive_content_detected`
- `raw_provider_logs_exposed`

The replay export redacts command and failed-command output summaries before they are written to the
report. Raw provider logs are not exposed in the replay payload.

`claims` attaches confidence and evidence refs to key evaluator facts:

- verification verdict
- signer authenticity
- signer trust
- quality gate statuses
- policy check statuses
- privacy redaction
- final outcome

## Outcome States

`outcome` gives a final state that downstream loops can consume directly:

- `completed`
- `completed_with_gaps`
- `failed`
- `abandoned`
- `committed`
- `needs_human_review`

The outcome is derived from replay evidence:

- non-finalized sessions are `abandoned`
- failed commands, failed gates, or failed policy checks are `failed`
- finalized sessions with gaps or warnings are `completed_with_gaps`
- finalized sessions with a commit command and otherwise clean evidence are `committed`
- finalized sessions without failures or gaps are `completed`
- unverifiable authenticity stays separate as `needs_human_review`

## Compatibility Rules

Replay schema changes are additive.

- Existing verifier fields stay present.
- Stable ordering and evidence refs are preserved for a fixed session.
- Missing provider evidence becomes a gap, not a crash.
- Bundle output remains artifact-only and includes `replay.json`, `receipt.json`, `manifest.json`,
  `events.jsonl`, `diffs/final.patch`, and optional normalized provider traces.

## Further Reading

- [AgentReceipt README](../README.md)
- [Current product requirements](./PRD.md)
- [Technical specification](./TECH_SPEC.md)
