# Soft-quota clarification repair — author delivery

This is the current author implementation delivery, replacing f430d9d3 and e2e5cf54.
Review is requested from opus50ts; no independent PASS is claimed.
Repository: `git@git.code.tencent.com:wego-ai/dingwei.git`.
Ref: `review/coder/node-soft-quota-impl`.
Baseline: r21 `8944a7c12198129af74fbe9c7ad6d646ad5c5d24`.
Actual code/test pin: `b8074831205c25376cf777c347ebdc985f3e4816`;
tree `5cb190be8c38e04f81d7f53f546d56cfa87e680a`.
Later commits add evidence only. Delivery ETA was 2026-10-17T10:00:00Z.

The clarification authority is reviewer evidence `ca5b39649611e7b25d7e66c4f0b0520a2d9f50e7`,
`review/opus50ts/seat-handoff-20261014`,
`docs/reviews/opus50ts-node-soft-quota-impl-e2e5cf54/REVIEW.md`.
It is a contract clarification, not a pre-signed implementation PASS.

## Changes and contract

**Emergency bytes only.** R = max(configured byte reserve, configured percentage
of the actual filesystem total). Overflow-safe ceiling arithmetic avoids early
integer truncation. `EmergencyReservePercent` is an author-chosen node setting,
1..99, default50; the default therefore implements R/2. The comparison uses
Bavail-derived available bytes and strict `<`; equality does not trigger an
emergency hold, although ordinary R admission still rejects insufficient headroom.
The former extra inode emergency stop is removed. The original I inode reserve
continues to block writer admission and has its own positive/negative test.
Legacy Hub reason strings remain accepted for precursor receipt compatibility;
the new monitor generates neither `node_inode_emergency` nor `node_space_unknown`.

**Unknown is not zero.** A first failed/invalid node snapshot immediately marks
new creation/start admission unavailable and emits a durable, deduplicated
`usage_warning`. Running seats are not stopped merely for that first error.
If the last successful reading is older than60s (default MaxAge), the monitor
sets `usage_unknown` holds and stops the affected recorded running seats while
preserving data. Exactly60s is not beyond60s. A root filesystem snapshot affects
all registered seats; a seat-usage error affects that seat's freshness.
Recovery within grace clears the warning; recovery never automatically releases
a hold. A successful old value is retained as a labeled historical metric,
never reused as fresh headroom. `disk_snapshot_valid=false`, its last-good time,
`soft_ready=false` and warning/hold flags expose that distinction.

Both the last-good timestamp and the initial-unknown anchor are private durable
state. Restart cannot grant a fresh grace period. An unreleased precursor state
missing both anchors fails closed unchanged; there is no automatic reset,
clearing or migration. The existing Active marker and recorded-container recovery
remain conservative after an unclean exit. There is still no hold-release API.

**Bounded, independent workers.** There are fixed scan, node-sampler, reserve and
enforcement loops (three spawned monitor goroutines plus the caller's reserve
loop), with coalesced enforcement signals. The sampler alone performs statfs,
resource probing and directory inventory; all run outside the quota-state mutex.
Admission and capacity reporting use timestamped caches only. A blocked phase
exceeding ScanBudget marks the sample unavailable without blocking reserve,
admission, reporting or the later age-based stop. No goroutine is spawned per
tick or per admission to replace a blocked filesystem operation.

Private-state writes use one serialized persistence worker. State serialization
is protected, but filesystem writes/fsync/rename and waits release the state
mutex. `StateWriteBudget` / `NODEAGENT_STATE_WRITE_BUDGET` is an additional author
setting (default5s, >0 and <=30s, bound into policy revision). Timeout/failure
makes future admission/reporting fail closed; it does not create another writer.
The enforcer attempts durable intent before Engine calls; if persistence fails,
it still stops writers while the previous durable Active marker is retained.
A timed-out IO operation may finish later, so its controller retains the lifetime
file lock until the actual worker exits. A replacement controller cannot race
that late write. Closing is bounded; it does not silently delete the state.

The per-seat start/stop operation gate, metadata revalidation and independent
stop batches from the previous repair remain. A late start cannot escape an
already-set hold, and a blocked start on one seat does not serialize other stops.
These claims concern process scheduling and local fixtures, not kernel IO
preemption, an external watchdog or a hard disk-usage guarantee. A syscall stuck
inside the kernel can retain its single worker and delay joining shutdown.
No unconditional disk-overrun or outage bound is promised.

**Hub/Console notification.** Authenticated `usage_warning` receipts are audited
and ACKed idempotently, but do not transition a running seat to a held/abnormal
state. Console distinguishes “磁盘测量暂不可用，暂停新建与启动” from a near-limit
warning; a real hold retains priority. No new owner credentials or network path
are introduced.

All other original implementation scope remains: explicit hard(default)/soft,
policy-bound registration/capacity/audit, soft consent and hard-first scheduling,
metadata-only allocated/apparent measurement, private durable holds, every writer
entry/replay gate, CPU quota/PID/log caps, read-only rootfs and bounded tmpfs.
Defaults remain conservative proposals subject to owner approval, not production
capacity promises. Original hard tests and33 baseline test files are unchanged.
No helper runtime, helper VERSION or dependency lock is changed.

## Fresh tests and exact attribution

| Check | Result | Raw evidence |
| --- | --- | --- |
| Full canonical incl. disposable local PG | rc0,99.785s; python48/shell2; PG parent+5 subcases pass | raw/canonical-b807.log |
| nodeagent full | rc0,140 top-level/no skips; package5.638s/wall7.587s | raw/nodeagent-b807.log |
| nodeagent full race | rc0,140/no skips; package11.067s/wall12.018s | raw/nodeagent-race-b807.log |
| Restored withdrawal checkout, soft targets race | rc0,4.521s | raw/restored-io-targets.log |
| Soft Console DOM | rc0,12 assertions | raw/console-soft-quota-b807.log |
| Existing quota / lifecycle / model browser suites | rc0,15 /15 /8 assertions | raw/console-{quota,lifecycle,model}-b807.log |
| Related hosted/nodegate/cmd-nodeagent/workpulse packages | rc0,268/no skips; wall432.198s | raw/related-packages-io-final.log |

Except the explicitly identified related-package run, these commands executed
on b8074831. The related run belongs to b92a07b96cb75380d1bd9a636ccc3fbffa578d44;
all four tested directory trees are byte-identical to b8074831. SOURCE-PINS records
those exact tree identities. The only b92..b807 change strengthens the blocked
private-writer test; it changes no product bytes. No reading is relabelled as an
independent review or a run on a different source.

All33 original baseline Go test files across nodeagent/hosted/nodegate are
byte-identical, including the original100 nodeagent cases. Tests added by this
feature explicitly prime the new sampler/cache and assert first-warning/expired-
hold behavior; inode emergency assumptions from the unreleased precursor are
replaced by the accepted I-admission-only contract. No hard/XFS assertion changes.
The earlier two inherited hosted legacy browser failures remain disclosed in the
original delivery; these fresh four browser suites are not a claim that every
browser fixture is green. No full helper rerun or real image/host acceptance claim.

## Current-source withdrawal controls

`raw/IO-WITHDRAWALS.json` binds25 Go mutations to original/mutant SHA256 and exact
target tests. Every counted control returns rc1 from an unchanged test assertion;
no compiler failure, panic or suite timeout counts. They cover:

- holding the state mutex across statfs, resource probing, inventory or private IO;
- removal of the write deadline or fail-closed broken-state flag;
- immediate unknown stop, never-expiring unknown, reuse of old-good values,
  last-good age reset, missing-anchor acceptance, and missing first notification;
- removed byte emergency, erroneous equality stop, extra inode emergency,
  weakened inode admission and early-truncated actual-total reserve arithmetic;
- spawning a sampler each tick; scan-held state lock; scan/batch-stop budgets;
- removing late-writer stop ordering and serializing scan with reserve;
- converting warning into hold and hiding the unknown overlay in Hub handling.

`raw/IO-UI-WITHDRAWAL.json` adds one browser assertion-red control hiding the
unknown-specific text. Thus the current count is25 Go +1 browser, not a sum of
historical39/14 controls. Original sources are restored between mutations; the
restored soft targets run with race enabled; the restored Console fixture also returns rc0 (raw/console-soft-quota-restored.log). The probe fixtures block and release
only local test IO. They also check capacity responses, 60-second boundaries,
no duplicate worker accumulation, persisted anchor/restart behavior, and held
lifetime locks across delayed writes. Runner/withdrawal scripts are stored with
non-Go fixture suffixes; their local lab paths can be mapped in an independent
checkout without changing the product mutations or test assertions.

## Boundaries and cleanup

Only local fixtures/fake Engine loopback servers/browser fixtures and the required
disposable pgtest were used. Private HOME/cache/TMPDIR and a whitelisted environment
do not inherit external PG DSNs or real credentials. No host, real Docker, real
Agent, key, firewall, image/artifact/release build, offer/job, sandbox, pilot or
deployment was touched. Section7 root-trust/network/owner decisions remain gates;
configuration reference strings do not prove the actual host boundaries.
Dataconn four-seat freeze remains unchanged. Final owned-process/path/cache
cleanup is recorded in CLEANUP.json; shared module caches and other seats remain
untouched. The named-ref delivery remains subject to opus50ts review.

CURRENT.md now points here. The earlier reserve follow-up manifest changes only
its CURRENT.md pointer hash, preserving latest-checkout portability; its reports,
source pins and historical raw logs are unchanged and retain their actual pins.

Final cleanup: zero owned fixture processes; restored detached mutation checkout removed. Private cache879,003,378 bytes, private HOME83,357,075 bytes and disposable TMPDIR1,139,530 bytes were removed; shared caches remain untouched.
