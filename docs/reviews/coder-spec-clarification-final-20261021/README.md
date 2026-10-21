# S-SPEC-CLARIFICATION-THREAD — complete candidate for independent review

Frozen implementation: **7115b67946fee16e64cd76a68284a41773932f75**, tree **8e82db39df0340692528454a22bf62853ad0f652**, immutable Git tag `evidence/coder/spec-clarification-source-7115b67`. Approved design **8d02f432**, base r33 **ab0f2144**. Reviewed D3 outbound switch **555a33bb** is included by conflict-free merge **2495115f**. Subsequent final-delivery commits contain evidence documents only. This is the author's candidate/evidence, **not independent PASS or deployment**.

Implementation covers typed append-only spec replies, PM plus one preappointed deputy, explicitly non-authoritative Agent material, corrections/confirmations as new floors, immutable spec revision/SHA/entry anchors, D1/D3 scope-change flow and D2 wake/limits; human Console and Agent HTTP/MCP; native authenticated Feishu reply identity/tenant/app/exact-parent binding and durable two-ID replay protection; mobile login return/weak-network submission/attachments; bounded task/spec/project_context projections. See the implementation checkpoint notes for file/function boundaries and the accepted [rollout impact contract](../coder-spec-clarification-implementation-20261021/ROLLOUT-IMPACT.md).

## Final local gates

| Gate | Result | Evidence |
|---|---|---|
| Default parallel full Go, strict embedded PG | rc0; 66 packages: 53 tested pass, 13 no-test packages | raw/final/go.jsonl, stderr, rc; GO-PACKAGES.json |
| Go vet ./... | rc0 | raw/final/vet.log/rc |
| Clarification race, five affected packages | rc0 | raw/final/race.log/rc |
| Canonical | rc0, python=56/shell=2 | raw/final/canonical.log/rc |
| Helper, env-i isolated HOME, locked venv | rc0, 787 tests, no unittest skips | raw/final/helper.log/rc |
| Eight browser suites | all rc0: original six 13/15/11/220/11/13; new thread22, mobile flow27 | browsers/; INPUT-EQUIVALENCE.json |
| Withdrawals | 38 original-green/designated-red/restored-green | withdrawals/MUTATIONS.json plus 114 pairs of raw stdout/stderr |
| Old r33 / migration / restore | rc0, actual old Hub loopback reads new copy; immutable data retained; failed0062 rollback; repeat backfill stable59→62 | compatibility/ |
| Exact build.sh | rc0, full40-character source ref, four local binaries | raw/final/build-final.log/rc; CHECKS.json hashes |
| Runtime dependency lock | validation rc0; unchanged lock | raw/final/runtime-lock.log/rc |

Full Go began 2026-10-21T06:16:09.073213999Z; final hosted package completed 2026-10-21T06:29:01Z. There are **39 individual optional legacy test SKIPs**, inventoried in GO-PACKAGES.json, not 0 SKIPs. New clarification tests are not among them. Full Go/helper/vet/race/canonical/build and compatibility use the frozen7115 source. Browser fixtures were run at0fe752b; the entire production HTML/JS and fixture input closure is identical at7115, with only a Go store test changed in the broad roots. This limited reuse is explicit; no browser final source change was skipped.

Real embedded-PG tests observe binding deletion waiting on the identity lock until the SQLite post commits; revoke-first waits then returns403, no second floor. Other tests cover changed PM/deputy/member grants, atomic audit/D2 failures, exact parent persistence failure/early replies, old untyped writes, same-ID replay vs equal-text same-parent supplements, read/status isolation, private file ACL/ownership and cursor invalidation after completed-task discussion. These are synthetic local tests, not a live provider claim.

## Rollout and limits

- Version pins root/Go/helper/MCP are **0.2.3.8**; runtime lock unchanged. Native trusted WS is implemented; unsigned/unverified FCS/webhook/fake input cannot claim reply authority. Both clarification switches default off and are independent of the reviewed D3 outbound flag. Application and non-secret tenant authority are pinned at startup; current PG binding/user/member state is locked and rechecked before SQLite commit.
- SQLite0061–0063 are additive (PG unchanged), ledger59→62. Published0061/0062 bytes retained. 0063 carries old owner-selected PM only, no inferred deputy or historical re-authoring. Local repeat migrate/backfill and failure rollback exercised.
- Per ruling92, the eventual same-batch Hub/file-service rollout and later per-seat helper upgrades are handled by leader/prodops. This candidate performs none of them. File-service has its own private SQLite image table; backup/path-outside-public-root/readback/rollback requirements are in ROLLOUT-IMPACT. Accepted images must not be stranded by reverting that service. The old0.2.3.7 question create/input incompatibilities and stock0.2.3.6 legacy-datad context are explicitly listed there.
- Real Feishu send/reply and a real Feishu in-app browser have **not** been exercised. Viewport390/820 plus normal Chromium login/readonly/derive/attachment/weak-network flows are local fixtures; screenshot script text is an intentional escaped-XSS fixture, not production content. Native Feishu image messages are not explanation bodies; use authorized mobile Console attachments. WebP has structural bounded validation, not full bitstream decoding.
- Private images have no public permanent link or automatic cleanup, max5MiB/four per post and a separate1GiB fail-closed store cap. Existing-service failure never silently falls back to public URLs. Without configured private storage selected failed/incomplete attachments block submission; clear them to post text.
- Old r33 read compatibility is not old-writer authorization. Freeze writes/workers and preserve DBs/outboxes/unknown receipts before rollback; old binaries ignore new switches. No rollback/deployment/host action occurred.

## Earlier results retained

raw/prior contains the first helper787 run's missing new-tool matrix error; old tests/fixtures while untyped-write semantics were narrowed; initial Go02a route-sweep failure (new98-vs84 inventory and auth/blank-page expectations); canonical55-vs56 count failure; source0fe subsequent full-Go/helper success. Earlier mutation runner missing module cache/wrong unittest class are harness errors, **not** valid reds. The first post-delete trigger withdrawal survived because FK refusal masked it; the final synthetic test isolates FK redundancy and all four append-only update/delete withdrawals now hit their specified markers. The build attempt with a short SOURCE_COMMIT was rejected rc2 by the build entry; corrected full40-character build is rc0. No prior failed result is rewritten into PASS.

SOURCE-MANIFEST.json hashes frozen Git blobs. SHA-MANIFEST.json covers retained evidence files. `python3 VERIFY.py` verifies both against the independently fetched Git source; [REPRODUCE.md](REPRODUCE.md) lists commands/environment and limitations. Compiled binaries, synthetic databases, private state, credentials and package bodies are not committed. Reviewer: leader; independent crosscheck: prodops.
