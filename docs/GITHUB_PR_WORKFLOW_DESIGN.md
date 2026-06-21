# GitHub PR Workflow Design

## Purpose

This document defines GitHub pull request workflows for AgentReceipt without implementing a GitHub App, failing CI gates, hosted storage, or team policy enforcement in the current release.

The design keeps AgentReceipt receipt-first:

- Developers keep running agents and AgentReceipt locally.
- PR surfaces show signed evidence, not agent rankings.
- CI-assisted checks verify deterministic artifact properties.
- Raw prompts, raw provider logs, and raw tool outputs are not uploaded by default.

## Workflow Variants

### Variant 1: Local-Only PR Comment

The local-only workflow is the current supported path.

Inputs:

- finalized local AgentReceipt session;
- `receipt.json`;
- `receipt.md` or generated PR Markdown;
- local GitHub CLI authentication when using `agentreceipt pr comment`.

Developer commands:

```bash
agentreceipt review --pr
agentreceipt pr comment
```

Outputs:

- reviewer-focused Markdown in the terminal or PR comment;
- receipt verification state;
- detected commands, risks, warnings, and reviewer checklist;
- no GitHub Check status.

Trust boundaries:

- AgentReceipt verifies local artifacts before rendering.
- GitHub receives only the generated Markdown body.
- Reviewers should treat the PR comment as a summary and use attached or shared artifacts when they need independent verification.

Data that leaves the machine:

- PR Markdown only.
- No raw provider logs, raw prompts, full transcripts, local key files, or blob store contents are posted by default.

Failure behavior:

- Invalid receipts should be shown as invalid in the Markdown.
- Missing provider evidence should be shown as a confidence warning, not as a posting failure.
- `agentreceipt pr comment` should fail only when local rendering or `gh pr comment` fails.

### Variant 2: CI-Assisted Receipt Check

The CI-assisted workflow is a future design path. It verifies receipt artifacts that the developer intentionally commits, uploads, or otherwise makes available to CI.

Inputs:

- receipt artifact bundle described in the contract below;
- checked-out pull request source;
- base and head revisions from the CI environment;
- optional repository policy file once policy configuration exists.

Outputs:

- GitHub Check result or CI job summary;
- deterministic pass/fail/warn findings;
- links to retained receipt artifacts when the repository chooses to upload them;
- no hosted AgentReceipt dashboard requirement.

Trust boundaries:

- CI must verify artifact integrity using embedded signature metadata before trusting receipt contents.
- CI must compare the receipt final diff against the checked-out pull request diff before reporting success.
- CI must not trust local absolute paths, local key directories, or provider records without validation.
- CI policy decisions are repository-owned and deterministic.

Data that leaves the machine:

- Only the artifact bundle that the developer or workflow explicitly provides.
- Raw provider logs and transcript files are excluded by default.
- Large blobs are excluded unless the repository explicitly opts into retaining them.

Failure behavior:

- Invalid event chain: fail the check.
- Invalid or missing receipt signature: fail the check.
- Final diff mismatch: fail the check or block merge under strict policy.
- Missing tests, lint, or typecheck evidence: warn by default; strict repositories may fail.
- Sensitive path changes without relevant test evidence: warn by default; strict repositories may fail.
- Missing provider evidence: warn by default because provider capture is enrichment.

## CI Receipt Artifact Contract

A CI-assisted check needs a minimal portable artifact bundle:

```text
agentreceipt/
  manifest.json
  events.jsonl
  receipt.json
  diffs/
    final.patch
```

Required artifacts:

- `receipt.json`: signed receipt containing the event chain hash, manifest hash, final diff hash, signer public key, and signer key ID.
- `events.jsonl`: append-only normalized event log used to recompute the event chain hash.
- `manifest.json`: session metadata and artifact references used to recompute the manifest hash.
- `diffs/final.patch`: finalized diff used to recompute the final diff hash and compare with the pull request diff.

Optional artifacts:

- `receipt.md`: human-readable local summary, useful for upload but not authoritative.
- `review.md` or PR Markdown: rendered output, useful for review but not authoritative.
- redacted provider parse reports: useful for diagnostics when retained intentionally.

Excluded by default:

- raw provider logs;
- raw prompts;
- raw tool outputs;
- transcripts;
- local private keys;
- local blob contents unless explicitly referenced by policy.

CI verification sequence:

1. Load `receipt.json`, `manifest.json`, `events.jsonl`, and `diffs/final.patch`.
2. Validate JSON schemas, reject unknown top-level receipt fields, and reject unknown critical receipt versions.
3. Recompute the event chain hash from `events.jsonl`.
4. Recompute the manifest hash from `manifest.json`.
5. Recompute the final diff hash from `diffs/final.patch`.
6. Verify the receipt signature with the embedded signer public key and signer key ID.
7. Compare the final diff against the pull request diff.
8. Evaluate deterministic policy checks.
9. Render check annotations and a concise job summary.

Current implementation spike:

- `agentreceipt verify bundle <path>` performs the local artifact integrity checks for `receipt.json`, `manifest.json`, `events.jsonl`, and `diffs/final.patch`.
- It verifies embedded signer material and does not require the signer's local key directory.
- It does not yet compare against a GitHub pull request diff, publish a GitHub Check, upload artifacts, or enforce repository policy.

Local deterministic diff comparison is also available through:

- `agentreceipt verify diff --bundle <path> --against pr.patch`
- `agentreceipt verify diff --session <id> --against merge-base`

Current implementation checks final patch integrity first, then compares candidates with
`agentreceipt verify diff` semantics for workflow-local parity checks.

## Deterministic Policy

The workflow must not compute an agent trust score, model score, or developer score.

Allowed policy checks:

- require a valid receipt signature;
- require an intact event chain;
- require manifest and final diff hashes to match receipt contents;
- require the receipt final diff to match the pull request diff;
- warn or fail when sensitive paths changed without test evidence;
- warn or fail when code changed without lint, test, or typecheck evidence configured for the repository;
- warn when provider evidence is missing or low confidence;
- warn when command results are unknown for commands that appear to be tests or deploy steps.

Disallowed policy checks:

- ranking a model, vendor, agent, or developer;
- treating a provider name as proof of safety;
- uploading raw prompts or provider logs to decide trust;
- silently changing repository branch protection or GitHub settings.

## MVP Acceptance Criteria

The GitHub PR workflow MVP is complete when:

- local-only `agentreceipt review --pr` and `agentreceipt pr comment` remain the supported baseline;
- CI can verify a portable receipt bundle without the signer's local key directory;
- CI reports deterministic pass/fail/warn findings from receipt integrity, diff match, and policy evidence;
- PR output excludes raw prompts, raw provider logs, transcripts, and local private keys by default;
- documentation clearly states whether enforcement is local-only, CI-assisted, or hosted.

## Open Questions

- Should CI consume receipt artifacts from the repository, uploaded workflow artifacts, or a signed PR comment attachment?
- What path convention should repositories use for committed receipt bundles?
- Should strict policy be configured in this repo, in global AgentReceipt config, or both?
- Should a GitHub App be optional sugar over CI, or a separate hosted product tier?
- How should CI compare diffs for multi-commit PRs that are rebased during review?
