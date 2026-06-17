# Plan 015: Spike a CI receipt verifier

> **Executor instructions**: Follow this plan step by step. Run every verification command and confirm the expected result before moving to the next step. If anything in the "STOP conditions" section occurs, stop and report; do not improvise. When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat c5ab6b4..HEAD -- docs/GITHUB_PR_WORKFLOW_DESIGN.md internal/receipt internal/model cmd/root.go cmd/root_test.go README.md`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against live code before proceeding; on a mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: MED
- **Depends on**: `plans/004-make-receipt-signatures-portable.md`
- **Category**: direction
- **Planned at**: commit `c5ab6b4`, 2026-06-17

## Why this matters

The GitHub PR workflow design is complete, and portable signatures already landed. The next useful product slice is a local/CI command that verifies a receipt artifact bundle deterministically. This should be a spike-level implementation that proves the contract without adding a hosted app or enforcement policy.

## Current state

Relevant files:

- `docs/GITHUB_PR_WORKFLOW_DESIGN.md` - describes CI-assisted artifact contract and verification sequence.
- `internal/receipt/receipt.go` - verifies receipts using local session layout.
- `internal/model/model.go` - receipt and manifest models.
- `cmd/root.go` - CLI command tree.

Current excerpts:

```md
docs/GITHUB_PR_WORKFLOW_DESIGN.md
CI verification sequence:
1. Load `receipt.json`, `manifest.json`, `events.jsonl`, and `diffs/final.patch`.
2. Validate JSON schemas and reject unknown critical receipt versions.
3. Recompute the event chain hash from `events.jsonl`.
4. Recompute the manifest hash from `manifest.json`.
5. Recompute the final diff hash from `diffs/final.patch`.
6. Verify the receipt signature with the embedded signer public key and signer key ID.
```

```go
// internal/receipt/receipt.go:93
func Verify(ctx context.Context, options Options) (VerifyResult, error) {
    // verifies a receipt from AgentReceipt session storage
}
```

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Receipt tests | `go test ./internal/receipt` | exit 0 |
| CLI tests | `go test ./cmd` | exit 0 |
| Full tests | `go test ./...` | exit 0 |
| Smoke | `./scripts/smoke.sh` | exit 0 |
| Final gate | `make verify` | exit 0 |

## Scope

**In scope**:

- `internal/receipt/receipt.go`
- `internal/receipt/receipt_test.go`
- `cmd/root.go`
- `cmd/root_test.go`
- `README.md`
- `docs/GITHUB_PR_WORKFLOW_DESIGN.md`

**Out of scope**:

- GitHub App implementation.
- GitHub Actions workflow that enforces this repo's PRs.
- Uploading artifacts to external services.
- Strict policy evaluation beyond deterministic integrity checks.

## Git workflow

- Branch: `advisor/015-ci-receipt-verifier`
- Commit message example: `feat: spike ci receipt verifier`
- Do not push or open a PR unless instructed.

## Steps

### Step 1: Add artifact-bundle verification API

Add a receipt verification function that accepts a bundle root path instead of a repo/session pair. It should load:

- `receipt.json`
- `manifest.json`
- `events.jsonl`
- `diffs/final.patch`

It should verify the same deterministic properties as local `Verify`: event chain, manifest hash, final patch hash, receipt hash, and embedded signature.

**Verify**: `go test ./internal/receipt` -> exit 0.

### Step 2: Add a CLI spike command

Add a command such as:

```bash
agentreceipt verify-bundle <path>
```

or a subcommand under `verify`, if that better matches the existing CLI style. The command should print the same style of verification summary as `agentreceipt verify` and exit non-zero when invalid.

**Verify**: `go test ./cmd` -> exit 0.

### Step 3: Add fixture-driven tests

In receipt tests, create a temp bundle from an existing finalized test session. Verify:

- valid bundle passes;
- tampered `events.jsonl` fails;
- tampered `diffs/final.patch` fails;
- missing embedded signer public key fails unless legacy fallback is explicitly supported for bundles.

**Verify**: `go test ./internal/receipt ./cmd` -> exit 0.

### Step 4: Document spike boundaries

Update README or `docs/GITHUB_PR_WORKFLOW_DESIGN.md` to say this command verifies local artifact bundles and does not yet add GitHub Check enforcement.

**Verify**: `git diff --check -- README.md docs/GITHUB_PR_WORKFLOW_DESIGN.md cmd/root.go internal/receipt/receipt.go` -> no output, exit 0.

### Step 5: Full validation

**Verify**:

- `go test ./...` -> exit 0.
- `./scripts/smoke.sh` -> exit 0.
- `make verify` -> exit 0.

## Test plan

- Unit tests for bundle verification success and tampering failure.
- CLI test for valid and invalid bundle command exit behavior.
- Smoke update only if the command becomes part of basic CLI workflow.

## Done criteria

- [ ] A local artifact bundle can be verified without the signer's local key directory.
- [ ] Tampering with events, manifest, final patch, receipt, or signature fails verification.
- [ ] CLI output is deterministic and non-hosted.
- [ ] Docs clearly state this is not a GitHub App or enforcement gate yet.
- [ ] `go test ./...` exits 0.
- [ ] `make verify` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

Stop and report if:

- Bundle verification requires changing the existing receipt JSON schema.
- The current manifest does not contain enough artifact information to verify a bundle without guessing paths.
- A CI policy engine becomes necessary to finish basic integrity verification.

## Maintenance notes

Keep this as a deterministic verifier. Policy checks such as required tests or sensitive-path enforcement should remain a later layer after artifact integrity is proven.
