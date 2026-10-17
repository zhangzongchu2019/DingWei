# Current delivery: A1+A2+A3, I and completed N1

Repository `git@git.code.tencent.com:wego-ai/dingwei.git`, ref
`review/coder/node-soft-quota-impl`; reviewer opus50ts. Author delivery only,
not independent PASS or deployment authorization.
Actual code/test pin `f3e6448d80089a5328d015f97280e0b8388630b6`,
tree `21668fad8ea5d22db4808ec2552c8560b3d29669`.
Baseline r21 `8944a7c12198129af74fbe9c7ad6d646ad5c5d24`.
Delivery ETA2026-10-17T10:00:00Z. Later commits contain evidence only.

## Latest instruction and fixed scope

The latest direct leader instruction, received after the earlier final-E handling,
restores D/A1 to this delivery and requires A1/A2/A3 review together; B remains a
separate next increment. It takes precedence over the earlier E instruction to
defer A1. Therefore A1 is restored; completed N1 is retained; N2 remains deferred.
The observed Git rulings object is11d649934352c0cbcbc5efa7b2d1b38bbb2cefd8,
`review/opus50tc2k/rulings`, `docs/rulings/S-NODE-SOFT-QUOTA-20261017.md`.
It contains D and the earlier E text; this report does NOT falsely claim that the
file's E section itself says to retain A1. The most recent direct leader message
is the authority for this restoration. No further scope flip is inferred.

No force push/history rewrite: f713 inode implementation was retained in Git,
reverted during E handling, then restored verbatim by this source commit.
The older follow-ups retain their actual pins/authority at their delivery times;
their opposite A1 scope statements are historical, not the current contract.

## Exact component correspondence

SOURCE-PINS independently binds:

- entire nodeagent subtree and node config to exact f713418da0166620da1859897410c0054393bcc8;
- entire cmd/workpulse subtree to exact4d2f408441bd867dc6cdb64ad21928c0ed2c468c;
- hosted/nodegate/m8/helper trees to17115394, unchanged;
- all33 original baseline Go test files unchanged.

The complete non-document difference vs17115394 is seven files: the five inode
implementation/config/test files and the two N1 CLI/test files. There is no
additional integration logic or helper-version/dependency change.

## Behavior retained and restored

**A1 leader-added scope:** independent InodeEmergencyReservePercent /
NODEAGENT_INODE_EMERGENCY_RESERVE_PERCENT, default50 and valid1..99, revision-bound.
I=max(fixed reserve, ceiling percentage of actual filesystem inode total).
Fresh free inodes strictly below ceiling(I × inode percent /100) cause durable
node_inode_emergency holds and recorded-container stops, retaining all data.
Original I admission remains; equality need not emergency-stop but can still
reject a new writer. Byte EmergencyReservePercent remains independent; byte
R/max/actual-total/Bavail/strict comparison is unchanged. Recovery/restart never
automatically releases a hold. No new state reset, deletion or migration.

**A2/A3:** first invalid/failed statfs warns and rejects new writers; it is not
F=0. Last-good age beyond MaxAge(default60s) yields usage_unknown; persistent
age anchors prevent renewed grace at restart. Fixed workers sample statfs,
resources/inventory outside the state mutex; cached admit/report do not block.
A single private-state writer has a bounded deadline, fail-closed behavior and
retained lifetime flock until actual completion. No per-tick replacement workers.
Per-seat writer/stop ordering remains. Kernel-stuck IO can retain its fixed worker
and delay joining shutdown; no kernel preemption/external-watchdog/hard-overrun
or unconditional outage guarantee is made.

**N1:** actual hosted-admin register-node flags --quota-mode and
--quota-policy-revision pass to existing RegisterNode -> normalize -> persisted
labels/audit. Soft policy requires approved lowercase64-hex revision; mode/default,
invalid/missing revision and labels-only activation guards remain. No new API,
auth route, direct SQL registration, account-key handling or retry. As before,
real hosted-admin opens/configures its DB before flag parsing; this is not a
read-only production preflight, and no real invocation is authorized here.

## Fresh combination gates and red controls

| Combination gate | Result | Actual wall / package seconds |
| --- | --- | --- |
| Complete canonical incl. disposable PG parent+5 | rc0,python48/shell2 | 254.322 / m8 fixture18.636 |
| nodeagent full | rc0,142 top-level/no skips | 92.759 /11.665 |
| nodeagent full race | rc0,142/no skips | 170.729 /30.062 |
| Hosted-admin full scenarios + N1 | rc0,4 top-level/no skips | 201.773 /16.340 |
| Restored inode target / N1 CLI | rc0 | 2.105 /6.158 wall respectively |

RESULTS.json binds actual commands/timings to f3e6448d. Complete canonical48+2
includes disposable local PG parent+5; full nodeagent142 and full race142; existing
hosted-admin suite plus the CLI-to-normalize case; restored relevant target runs.
No component run is relabelled as execution on this new combination.

Four fresh mixed-source controls are actual unchanged-test assertion rc1:
remove inode emergency, share its byte percentage, restore the legacy CLI,
and remove N1 revision forwarding. No compiler/panic/suite-timeout red counts.
Sources are restored between mutations; restored targets are green.

The broader33 Go+1 browser inode/IO controls belong to f713, and the three N1
controls belong to4d2, kept in their earlier reports. Exact component tree
identities justify reuse of those scoped proofs; they are not added to a new
current-execution count. Inode config/boundary/default/range/revision/odd-reserve,
byte independence, I admission, persistence/stop/data retention/restart cases and
A2/A3 concurrency tests all execute in the fresh full nodeagent suite.
Original100 nodeagent cases and33 baseline test files are unchanged.

## Deferred and operational limits

N2 remains a known follow-up: short-lived login containers are not fully inside
the soft usage/recorded-container hold/stop lifecycle. Existing preflight is not
full lifecycle accounting. It is not silently claimed fixed by this combination.
B capacity rules (minimum1core/1.5GB/40GB, three-dimensional capacity, unmanaged
load/unknown, admission and Console explanations) are not started; separate
branch only after this candidate PASS. Existing inherited browser limitations
retain prior source attribution; no blanket browser or full-helper green claim.

Only local synthetic fixtures/fake Engine/browser servers and expressly required
disposable loopback pgtest. No host, real Docker, external PG, real key/credential,
Agent, firewall, release/image/artifact build, offer/job, sandbox/pilot/deployment.
Trust/network/owner gates and dataconn freeze remain unchanged. Cleanup records
only owned private processes/cache/HOME/TMPDIR/mutation checkout, with shared
caches and other seats untouched. Manifest keys are repository-relative; earlier
wrapper hashes update only the current-pointer chain, not historical logs.

Final cleanup: zero owned fixture processes; cache714,677,571 bytes/HOME83,050,385 bytes/TMPDIR569,765 bytes and restored mutation checkout removed. Shared caches/other seats untouched.
