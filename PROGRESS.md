# Replay Evaluator Contract Progress

Plan source: [`PLAN.md`](./PLAN.md)

## Status

- Current status: Step 0 complete
- Next step: Step 1
- Last updated: 2026-06-21T08:20:00Z
- Validation results:
  - `test -f PROGRESS.md`
  - `test -f CHANGELOG.md`
  - `make fmt-check`

## Step Checklist

- [x] Step 0: Progress and Changelog Tracking Setup
- [ ] Step 1: Define Explicit Replay Verification Verdicts
- [ ] Step 2: Add Local Trust Policy Evaluation
- [ ] Step 3: Harden Signer Material and Legacy Migration Semantics
- [ ] Step 4: Add Evaluator Scoring Signals
- [ ] Step 5: Add Quality Gate and Command Failure Evidence Schema
- [ ] Step 6: Add Patch Semantic Summary
- [ ] Step 7: Add Policy Checks and Review Focus
- [ ] Step 8: Add Privacy Report, Claim Confidence, and Outcome Classification
- [ ] Step 9: Document the Production Replay Evaluator Contract

## Implementation Notes

- Keep additive replay report fields for compatibility with existing consumers.
- Preserve existing `Valid`/`SignatureValid` semantics in replay JSON for compatibility while introducing new verdict fields.
- Keep replay as an evidence-only report and run only local, deterministic analyses.
