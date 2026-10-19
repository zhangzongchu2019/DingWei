# R32 R8 — Agent spec review and human-confirmed acceptance criteria

Author implementation candidate; required gates completed. Design: `c2ed9a23ffa44b52dd10163d089bbe1288e784d8`, independently passed in `f2efca31db32787d0a2121c1171801bee2dfd29c`. Frozen implementation/test source: `62d3bdb98b6391519dc011663061ff5dd069cec7`, tree `ee793d4b6c6373fef9e841c5b66e5c2ad4947260`, based on R9 PASS `9c79f269b2ff8f4c3fef1e38bc2c59b14b07f04b`. The evidence-only successor is reported in the delivery message. This is author evidence, not independent implementation acceptance or deployment permission.

## Result and scope

W8.1–W8.3 / A23–A25 are implemented. The current human editor selects their own active online Agent. Agent issues enter the current revision's gate as pending, with the actual authenticated Agent recorded. Humans resolve each issue and respond; an empty authenticated issue list also requires explicit human confirmation. Agent credentials cannot invoke human review/finalization.

Criteria replies are bounded proposals. They preserve the original content/anchors and remain uncommitted until the current human editor confirms the exact revision/hash. Confirmation adds immutable revision n+1 in editing state, retains old revision/gate history, and requires a fresh authenticated review and human confirmation. Fresh-review requirements follow later human revisions too. The request ID is the per-attempt random nonce/generation; Hub boot identity separately rejects pre-restart work.

During pending review/criteria or criteria confirmation, SQLite triggers block the existing write paths, including old binaries. An explicit cancel-and-transfer validates the target and snapshot, cancels work, releases the lock, transfers the editor, and audits in one transaction. Late results cannot persist or fetch a cancelled snapshot. The receiving editor starts a new request with their own Agent. Histories beyond 100 rows cannot hide the current request: human requests are active-first/latest-first; Agent listings retain bounded stable-ID pagination and request-ID lookup.

## Main paths

- `internal/store/migrations/0056_console_spec_agent_review_r32.sql`: additive SQLite ledger migration, four tables, indexes and write/fresh-review triggers. No identity/hosted PG schema changes.
- `internal/store/console_spec_agent_review.go`: request state, bounded proposals, callback provenance, human confirmation, atomic cancellation/transfer and expiry/restart reconciliation.
- `internal/identity/spec_review_authorization.go`: current owned active Agent and owner locks in an ordinary read/write PG transaction with a precommit connection check. No task/group grant is added. PG/SQLite are not a distributed transaction; ambiguous write replies report unknown and must be queried using the original request ID.
- `internal/m8/console_spec_agent_review.go`: human Console endpoints; dedicated Agent bearer GET/POST; authenticated WS results; capability-gated durable envelopes. Auxiliary `spec_review_type` survives the reliable transport's `type=delivery` rewrite. Input and route guards reject forged Hub request types. Drain checks current capability, request state, deadline and boot before resending.
- `tools/sessionhelper/sessionhelper/spec_review.py`: fixed-Hub HTTP transport, no redirects or credential arguments; trusted system requests become a bounded instruction containing only the nonce. Untrusted spec text is fetched as data through current authorization. The reply uses dedicated MCP/HTTP, without ordinary automatic replies.
- `dw_spec_review_requests`, `dw_spec_review_reply`: MCP → local IPC → fixed Agent HTTP. Root/Go/helper/MCP report version 0.2.3.6. Restricted IPC modes cannot use the new channel. No running helper was upgraded.
- `internal/m8/console_specs_page.go`: review/criteria request, pending lock, visible issues/candidates, explicit confirmation, cancellation and transfer. Human annotations cannot replace the authenticated Agent or issue text; the backend enforces this independently of UI controls.

## Verification

Frozen-source author gates completed 2026-10-19T11:47:11Z: full Go rc0, 52 tested packages passed and 13 without tests; vet rc0; canonical Python51/shell2; targeted race three packages rc0; 30 designated withdrawals passed; ten browser suites rc0; compatibility producers rc0. Helper full suite:744 passed in the locked environment. Browser suites are reused under the whole-input Git blob proof. Exact receipts are in `GATES.json` and `GO-PACKAGE-LEDGER.json`; predecessor failures remain preserved.

The deterministic race producer uses fixture-only SQLite audit triggers and a process-initialized scalar function to block a production transaction before commit. The competing operation must actually wait for the single SQLite connection (observed wait count 0→1). Four cases prove review/criteria × cancellation-first/reply-first, with expected stored status and gate presence. This does not add a test hook to production methods. See `raw/r8-deterministic-delivery.log`; full frozen-source Go and race gates include these cases.

`withdrawals/WITHDRAWALS.json` records 30 actual isolated source withdrawals: positive rc0 → designated assertion red → restored rc0. The database write lease and callback identity each have multiple enforcing predicates; their cases withdraw the corresponding logical guard family together, rather than claiming an unrelated assertion as evidence. No compile/import/syntax failure counts as a designated red.

Browser evidence uses the production embedded HTML with HTTP fixtures. It tests the UI and is not backend authorization evidence. Real Hub HTTP, real WS reliable requests, store transactions and real PG lock/revocation tests provide separate backend evidence. Non-verbose full Go logs establish package results, not an individual test skip count. The final helper full suite runs in the locked venv. Browser gates are reused only under the whole-input Git blob equality proof in `REUSED-GATES.json` (the final follow-up changes only helper MCP result schemas and their unit test). No environment guard is disabled.

## Acceptance evidence map

| Scope | Main regressions / evidence |
|---|---|
| W8.1 authenticated review and human gate | `TestR8HTTPRealReliableReviewAndHumanOnlyFinalize`, `TestR8ActualReviewerPendingHumanGateAndReplay`, `TestR8EmptyIssueListStillRequiresHuman`, authenticated WS endpoint test |
| W8.2 proposal, exact anchors, new immutable revision, fresh gate | `TestR8CriteriaConfirmationFreshRevisionAndOldGateHistory` (editing/review), `TestR8CriteriaRejectsQuotesAndSpoofedConfirmation`, `TestR8FreshReviewRequirementFollowsHumanRevision` |
| W8.3 write lock, cancellation and atomic transfer | `TestR8PendingLockCoversAllStoreWrites`, `TestR8CancelTransferIsAtomicAndRejectsLateReply`, four deterministic SQLite commit orders |
| Current identity, stale reads, restart and timeout | real PG `TestR8OwnedAgentPGAuthorizationAndRevocationLock`; HTTP read-error/cancellation tests; `TestR8RestartBeforeReconcilerRejectsWSAndCriteriaConfirmation` |
| Helper/MCP/compatibility | locked 744-test suite; 0056 rollback + actual old-binary reader; exact old-helper capability probe; 30 specified withdrawals |

These are layered tests: HTTP integration uses the in-memory identity store, while the PG transaction/locking test uses real embedded PG. They do not claim a single end-to-end real-PG HTTP deployment exercise. Verbose targeted stdout is `raw/r8-targeted-delivery.log` (strict embedded PG, no skip recorded).

## Compatibility and rollback

The existing compatibility producer starts an actual exact-r31.1 Hub against a library upgraded by the frozen new store. The R8 supplement injects failure when 0056 is entered in the ledger: its tables/indexes/triggers roll back together, the old task remains, and removing the test trigger allows one successful upgrade. Subsequent startup skips the ledger entry. Exact 0.2.3.5 helper sources do not advertise the new capability/tools; the new helper does. Old-helper safety also depends on Hub capability admission and durable-drain checks, not on expecting unknown messages to behave safely by accident.

Old binary reads are supported by additive storage. This is not permission for an unrestricted live downgrade: an old binary does not understand R8 request state. Close the entrance and cancel/fail active work before rollback; active-request write triggers intentionally fail closed. Local probe executables, SQLite fixtures and helper state are excluded from evidence and Git.

## Preserved failures and limits

Predecessor receipts remain under `raw/` with their original rc. Early fixture errors included a reversed store-read argument order, an unsupported created_via value (the existing principal provenance enum is retained), duplicate UI panel IDs, and missing canonical/MCP test fixture updates. The first request transport was direct because generic system envelopes are excluded from reliable delivery; the final implementation explicitly admits the two trusted request types and preserves their semantic type in auxiliary metadata. These failed attempts are not counted as final gates.

The system-Python helper attempt lacked locked runtime dependencies (613 discovered tests, 24 failures/27 errors/50 skips); its rc1 is preserved. The first corrected locked environment ran 743 tests successfully; the schema follow-up adds one case and the final run records 744. Canonical's missing new-tool argument fixture and its stale count assertion were corrected; failed logs are retained separately.

The first c7 full Go attempt returned rc1 only in `internal/consolemux/TestBudgetRealPGDeadlineAndCapacity`, observing 16 active background SQL queries. The package, repository adapter and pgtest inputs are unchanged from R9. Root cause is not established. Supplemental R9 and candidate isolated count-three comparisons passed, as did the predecessor 3a14b469 full run. These do not replace the leader’s conditional r31.1 comparison: that comparison is required only if the final frozen-source full run repeats the failure. No environmental waiver or root cause is inferred from an isolated rerun.

No deployment, production traffic, real helper upgrade, host43/dw123 operation, source-object upload or binary upload is included. r31.1 `ef439b82` remains the sole P3/P4 production build source until separate leader authorization.

Independent intermediate dev-env evidence: reviewer branch `review/togpt60lunazzc/hosted-enable-r31`, commits `4bddfafe35bb9213114b920d5f223f1124ac741c` and `c8c0ba5fabcb5834d9b1fa0023e23a1981183760`, reports `docs/reviews/togpt60lunazzc-r32-task-flow/dev-env-3a14b469/REPORT.md` and `dev-env-3a14b469-rerun/REPORT.md`. Both targeted R8 store/m8/identity runs passed as ubuntu UID1000 with umask0002 and Go1.22.2. All Go inputs are unchanged from 3a14b469 to the frozen source. These are scoped intermediate reads, not final R8 implementation acceptance.
