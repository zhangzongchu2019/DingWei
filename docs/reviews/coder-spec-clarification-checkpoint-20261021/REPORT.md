# Spec clarification storage checkpoint — not a complete candidate

UTC: 2026-10-21T04:29:50.826292+00:00

Approved design: 8d02f432ca5983e1802afc4f0d6eeb2b7b16eaf2. Base r33 ab0f214496e6a5df1c71826cc978f40528923b8f. Frozen checkpoint source **2ec504c6cd7d8a2d012a2fc8ce14a906bff9638e**, tree **216a48a5674bf7f93c0750fb0b14ba835b29c420**. This is an early implementation checkpoint, not a final review request or deployment source.

## Implemented

- Additive SQLite 0061 ledger migration, current PM/one deputy policy and immutable policy history; owner selects PM, current PM alone appoints deputy, PM replacement revokes deputy immediately for future access.
- Per-spec transactional floors; metadata references the existing immutable D3 answer body rather than duplicating it. Correction and deputy confirmation append new floors; original body and metadata update/delete are refused by triggers. Old answers are not promoted into new public posts.
- Structured criterion/case/template anchors and SHA/line sidecars for old documents without IDs; explicit kind, current policy authorization, Agent technical-only classification, same-ID replay with payload equality, different-ID equal text preserved.
- Scope-change posts atomically create the existing D3 pending change. Active linked cards use the existing D2 input/wake and rate budget; continued resolved/done discussion does not reopen tasks or create a wake.
- Bounded question-thread reads, current PM/deputy and current Agent-role checks, no new grant over private old D3 answer bodies. SQLite transaction checks and caller-held PG identity callback fail closed. Once a policy is registered, old answer/input operations refuse missing-type/anchor legacy writes; disabling the eventual UI must not undo this enrollment guard.

## Verified scope

Go 1.22.2, GOTOOLCHAIN=local, GOPROXY=off, GOSUMDB=off; local VM-4-12-ubuntu UID 1001. Store package full test rc0 (20.884s); new clarification tests with race rc0; store vet rc0. Eleven top-level clarification tests include rollback injection, read revocation, replay/pagination, immutable history, done-state continuation, policy write failure, and a deterministic SQLite commit/revoke ordering test. This is NOT a real PG concurrency test; production PG/SQLite lock integration is still pending.

Seven withdrawal mutations on the frozen source each have baseline rc0, the named assertion red, and restored rc0. M1 authority, M2 equal-text supplement, M3 last identity check, M4 legacy-path guard, M5 pending-change gate, M6 revoked PM read, M7 spec-only reply budget. Raw commands, UTC, stdout/stderr and markers are under raw/MUTATIONS.json and associated files.

The first mutation attempt on 9b49f653 had M7 survive: the linked D2 card's independent budget masked withdrawal of the thread budget. Raw attempt is preserved under raw/FIRST-MUTATION-ATTEMPT. Test-only successor 2ec504c6 uses a genuine spec-only question without a D2 card; production code was unchanged. The final M7 is now a designated assertion red. No first-attempt result is rewritten as a pass.

To reproduce the seven mutations from Git, run `python3 docs/reviews/coder-spec-clarification-checkpoint-20261021/REPRODUCE-MUTATIONS.py <repo> <new-private-output-directory>`; it creates a private detached worktree and runs offline cached Go tests at the frozen source. It does not connect to a live service.

## Still required before a full candidate

No production HTTP route, Console UI, startup flag wiring, helper/MCP update or Feishu inbound path has been added yet. Complete the default-off flags and old-entry-point wiring, trusted SDK/FCS metadata durability and validation, exact notification parent mapping, double-ID replay and distinct-message supplements, early-reply reconciliation/receipts, current PG binding/member locks, mobile 390/820 and safe attachment/login/idempotency flows, all three read projections, migration/rollback compatibility and full candidate gates. SQLite 0061 remains provisional until integration number verification. Do not deploy this storage-only checkpoint.

The owner has already approved the next batch and real Feishu reply activation under ruling90; no repeated owner approval is sought. Actual production execution remains with the approved batch runbook and leader GO. Both code flags will default off. Original first implementation checkpoint ETA 05:30Z is met early by this source checkpoint; full candidate ETA remains 2026-10-21T18:00:00Z, subject to reporting any newly discovered scope issue.
