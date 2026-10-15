# S-A3-DIRECT — author implementation checkpoint

This is author evidence, not an independent review verdict or deployment order.
The branch is based directly on r12 `6c4600478a4c7c769cb6adaa1024e4504208f39b`.
Runtime implementation is `f902e649d12001773350d470c86d05bc15e5dc22`;
`6a7d1d8703113e00f393c1b9f52eedbab669ea5b` changes only two related tests.
Self-update work is on a separate branch and is not an ancestor.

## Behavior

The two Agent spec write routes authenticate the existing Agent bearer and
require an active Agent and active owner. The explicit `direct-v1` header selects
new helper semantics; it grants no identity or permission by itself. The server
supplies the actor, and strips client edit-capability context. Create and update
are audited as `agent_create` and `agent_update`. Full-spec update uses a dedicated
repository method that checks revision 1's `created_by_kind=agent` and current
Agent ID in the same SQLite transaction as revision creation and optimistic CAS.
A different owner, same-owner peer, human-created or unknown-origin spec is denied.

The approval state map, pending page/link and its three HTTP routes are removed.
Authenticated legacy WS request/poll receives `upgrade_required` immediately.
Old local IPC calls also return `upgrade_required` without HTTP or spec writes.
New MCP calls perform one direct Agent HTTP write, without grant/poll arguments.
O3's 35-second shim / 30-second helper / 15-second HTTP budgets and unknown outcome
with no blind retry remain. A real committed write whose response is dropped is
retained exactly once and returned as unknown, not a rollback promise.

Console deletion is reversible archival. Single and batch actions validate all
IDs and permissions and write all archive events in one transaction. An event
insert failure rolls back every row. Owner or management deletion authority is
required; restoration preserves revision/task history. Default list and board
hide archives; the explicit toggle and restore are available. Existing shared
historical reads remain readable and cannot edit/split while archived. Detail
shows persisted creator/latest-editor labels and revision times. No schema or
migration file changes. Task/inference capability routes and explicitly selected
inference-assist behavior are retained separately.

## Completed checks

Locked Python: 3.12.3, websockets 15.0.1, pexpect 4.9.0. Helper discovery uses isolated
HOME/TMPDIR with seat-specific environment removed. Browser runner uses Playwright
1.56.1 and system Chrome (`PW_CHANNEL=chrome`), private local HTTP except the P1
fixture's real isolated Hub/PG/Agent processes. No production endpoint or real
Agent is used.

- Full helper discovery: **652 tests, 351.292 seconds, rc0**.
- Canonical build on unchanged runtime source: **rc0**, `BUILD_TESTS_OK python=42 shell=2`.
- Focused `go test -race` on m8/store/workpulse direct authentication, ownership,
  archival, legacy response, real MCP E2E, private scans and route guards: **rc0**.
- Real MCP subprocess → Unix IPC → helper authenticated WS + Agent HTTP → SQLite:
  **9 E2E cases**, plus five private-stream scanner cases; included in race gate.
- New direct archive/detail browser: **8 rc0**; enroll **20 rc0**; workflow **12 rc0**;
  theme **rc0**; task board **48 rc0** (existing K13 deferred); broad Console **20 rc0**;
  Specs **220 rc0**.
- Real P1 Hub + PostgreSQL + Chrome + enrolled WS Agent: **29 passed, 0 failed,
  0 skipped, rc0**, including retained task capability isolation and revocation.
- `git diff --check`: **rc0**.

## Failed attempts and attribution retained

Initial full `go test -v ./... -count=1 -timeout 20m` returned **rc1**, not PASS:

1. `cmd/feishu-fanout/TestOpenSpoolPool_RefusesHubDB`: a pre-existing hardcoded
   migration-ledger list omits existing `0300_console_transport_slot.sql`.
   The exact r12 parent independently reproduces the same rc1. No fanout or
   migration product change is part of A3. Leader scope decision requested.
2. `TestNewPlatformRouteSweep`: this change adds three archive routes and changes
   direct Agent unauthenticated responses to 401. Its old count/status pins were
   missed initially, corrected in the test-only child, then targeted **rc0**.
3. `TestBudgetRealPGDeadlineAndCapacity`: connection timeout under concurrent
   full-suite load; serial exact-source rerun **rc0**, with zero remaining SQL.
4. Three m8 asynchronous fixture cases failed in the first broad run:
   `TestFeishuIngressNewChannelAutoBindingAndReplyToSender`,
   `TestSessionNameReconnectKeepsNameAndReleaseReuses`, and
   `TestProducerSessionMetadataAndOnlineDirectory`. Exact candidate and exact
   parent focused count3 reruns both **rc0**. These initial failures are retained,
   not retrospectively converted into PASS.

Initial broad Console run under full parallel load returned 18/20; exact parent
and candidate serial runs each 20/20. No Console assertion changed. Specs' original
readonly keyboard assertion raced its intentional broken-image fallback on both
parent and candidate. The related test now scrolls/awaits that image fallback
before taking the text snapshot; strict equality and all 220 assertions remain.
An initial attempt merely waiting without triggering the lazy image load timed
out and is retained privately. No product image behavior changed.

A separate parent hosted diagnostic used an intentionally short 2-minute limit
and timed out; it is not evidence of a product failure. Initial candidate full
hosted package completed rc0 in 485.796 seconds. A final serial full-Go run is
pending; do not read this checkpoint as full-Go green.

## Sensitive withdrawals

`evidence/withdraw.py` creates temporary Go overlays without editing the checked
source. Each case exits **rc1 at its intended behavioral assertion**, not a compile
error: bearer authentication; revision-1 Agent ownership; `agent_update` audit;
legacy upgrade response; old-HTTP protocol rejection; batch transaction rollback;
archive permission; restore; default hidden list; hidden board.

Restoring the retired pending entry in the real enroll page returns **19/20 rc1**.
Applying exact r12 Specs HTML to the new browser returns **rc1** (`0 != 2` deletion
controls). Removing only the helper legacy protocol/identity predicate in a
private module returns **rc1**, actual `ok` versus expected `upgrade_required`,
with actual local HTTP execution. The first pending-entry invocation used the
wrong mutation switch and exited 3; only the corrected `MUTATION=` invocation is
counted as a valid red.

## Compatibility and limits

Old helper → new Hub fails quickly with `upgrade_required` and does not write.
New direct helper → old capability-only Hub cannot use the new write protocol and
is denied; it must not silently fall back to approval or blind retry. Deployment
must coordinate helper capability with the adopted Hub. This branch does not
install, package-upload, deploy or operate any real Agent. The separately delivered
Docker update matrix has no real-container PASS locally and is unrelated to A3's
MCP/HTTP fixture acceptance.
