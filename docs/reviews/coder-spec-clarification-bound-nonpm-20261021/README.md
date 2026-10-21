# Bound non-PM Feishu reply regression supplement

Leader review of3748ee9a: PASS with this mandatory test-only supplement before integration. Frozen test source **a94070b18e1f1fd64817536390777f74f43c7177**, tree **e50d0d469928194b3067169d5eaa6dc5de14d853**, immutable tag `evidence/coder/spec-clarification-bound-nonpm-a94070b`. Base **3748ee9a**. TEST-ONLY-PROOF.json verifies the exact three-file test-only diff: one existing m8 fixture/test, one new store test, one test withdrawal program. **Product source, helper, protocol, flags and migrations unchanged.** Original complete evidence remains under ../coder-spec-clarification-final-20261021.

## Real bound identity negatives

`TestClarificationFeishuRejectsBoundNonPMMembers/{deputy,ordinary_member}` creates the real current bindings with CreateRegistrationCode/RedeemRegistrationCode, and explicitly asserts ResolveIdentity succeeds for active user lbl, type member. That user's active Agent is joined to the same task group. Current PM is zzc with its own distinct binding; deputy case preappoints lbl, ordinary case does not. Configuration occurs before creating the question so notice generation/policy/revision/SHA are current. One real fixture notice is sent and its exact provider parent is used.

For each bound non-PM: trusted fixture ingress is consumed, inbox records `rejected/current_pm_identity_required`, rejection receipt says not posted; reconciliation still leaves zero floors. The same issued parent accepts a current-PM positive (one new floor, source feishu, role pm). Deputy can then post an explanation through Console; ordinary member cannot (403). This is neither an unbound-subject denial nor a disabled/unmatched/expired-notice false positive. All provider IDs and messages are synthetic; no live Feishu action occurred.

## Single final-role withdrawal

`TestClarificationFeishuOriginRequiresPMAtAppendBoundary/{deputy,technical_reader,pm}` directly exercises the shared transactional append seam. The deputy is current and Console-authorized; the technical reader has an existing legitimate read/technical contribution grant. Their Console positive controls succeed, with first floor1 after rollback. The PM Feishu-origin control succeeds. Non-PM Feishu origins must return ErrTaskFlowPermission.

The full ingress has multiple independent earlier identity/subject/PM checks. Removing only the final role check there can still be masked by these earlier checks. This **lower-boundary** test deliberately isolates the final role constraint; it does not claim that production ingress lets a non-PM reach this seam. Production upstream guards remain unchanged.

`bound_nonpm_mutation.py` withdraws **only** `|| role != "pm"` at the reviewed append origin check (original line274), keeps source validation and all other code unchanged in a disposable worktree, and restores the original bytes. Two cases are each original rc0 → withdrawn rc1 with exact marker → restored rc0:

- `CLARIFICATION_FEISHU_NON_PM_ORIGIN_ACCEPTED_deputy`
- `CLARIFICATION_FEISHU_NON_PM_ORIGIN_ACCEPTED_technical_reader`

Raw stdout/stderr and commands are in withdrawals/. No compile error, unrelated assertion or redundant upstream refusal is counted as a red.

## Validation and review scope

On frozen a94070b, non-root, Go1.22.2/local/offline, strict embedded PG: full affected packages **internal/store, internal/m8, cmd/workpulse all rc0**; targeted race for both new tests rc0; vet all three packages rc0. CHECKS.json includes exact package durations and individual legacy skips; none of the new cases is skipped. Prior full repository53-package and helper/browser/canonical/38-withdrawal evidence remains the already reviewed baseline; this supplement does not relabel those as new runs. The initial pre-freeze smoke log is retained as such; final package/race/mutation evidence uses a94070b.

New verification first checks original source/evidence manifests, then the exact three-file test-only diff and its Git blobs, supplemental artifact SHA/size, and both role-only red records. Source/code remains exchanged solely through Git. No binary or package was uploaded; no production, seat, Hub,43/dw123 or host action was performed. Please review this supplement as the closure of the required test coverage item; it is not a new deployment authorization.
