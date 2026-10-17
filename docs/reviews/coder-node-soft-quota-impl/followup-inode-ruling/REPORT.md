# Current soft-quota delivery — independent inode emergency per leader ruling

This successor corrects A1 and replaces17115394/b8074831 as the final author
candidate. A2/A3 remain implemented. Independent opus50ts review is pending;
there is no author-issued review PASS, deployment GO or freeze change.

- Repository: `git@git.code.tencent.com:wego-ai/dingwei.git`
- Ref: `review/coder/node-soft-quota-impl`
- Baseline: r21 `8944a7c12198129af74fbe9c7ad6d646ad5c5d24`
- Actual code/test pin: `f713418da0166620da1859897410c0054393bcc8`
- Code tree: `33f07966487bd0983184b94a2793caa10d7997bf`
- Later delivery commits contain evidence only. Promised ETA:2026-10-17T10:00:00Z.

## Sole current authority and scope correction

`review/opus50tc2k/rulings` exact `eeb21f5bdce4795f75a35d4f0dbc0c8e03c3f627`,
tree `455ddb3c3702db8ffafb4d2cfa4398844397e19c`,
`docs/rulings/S-NODE-SOFT-QUOTA-20261017.md`, A1 requires an independent inode
emergency stop. **This is leader-added scope**, in addition to the original byte
R/2 and inode I admission reserve. The earlier IO-grace delivery's claim of
“no extra inode emergency” is superseded, not the final contract. Historical
reports and executions retain their exact source attribution.

This commit implements A1 only. Ruling A2 unknown grace and A3 lock-free IO/cache
remain. Ruling B capacity/minimum-seat rules are a later separate increment and
are neither implemented nor claimed here.

## Independent inode configuration and enforcement

`NodePolicy.InodeEmergencyReservePercent` /
`NODEAGENT_INODE_EMERGENCY_RESERVE_PERCENT` defaults to50, accepts integers1..99
only, and is included in the policy revision. It is not aliased to byte
`EmergencyReservePercent`. Invalid, negative, overflowed, or nonnumeric values cannot disable the protection; an absent environment value
uses the default, as with the existing configuration parser.

I = max(configured fixed inode reserve, ceil(configured inode percentage × actual
filesystem inode total)). The inode emergency line is
ceil(I × InodeEmergencyReservePercent /100). Quotient/remainder arithmetic avoids
overflow and early percentage truncation. A fresh valid snapshot with free inodes
strictly below that line holds registered managed seats as `node_inode_emergency`,
then enforces recorded-container stops. Intent is persisted before Engine calls;
files/data/identity are retained. Equality does not trigger emergency hold, but
original I admission remains required and can still reject new writers.

Byte R/max/actual-total/Bavail/strict-< behavior is unchanged. If both emergencies
coincide, existing first-hold semantics preserve the first durable reason; the
inode-only tests keep byte headroom ample to prove the independent path. Existing
holds survive recovery/restart and have no automatic release or reset. The Hub
already accepts/audits/ACKs `node_inode_emergency` through its authenticated,
idempotent path, so no new endpoint or credential route is needed.

The setting/default is a conservative operator proposal; final node values remain
owner decisions. Node policy revision changes require the existing registration
approval binding; a config reference is not host approval.

## A2/A3 preserved

Node first-error/invalid statfs warns and rejects new writers, without treating
one error as F=0. Last-good age >MaxAge(default60s) causes `usage_unknown` holds;
exactly60s does not. Persistent last-good/initial-unknown anchors prevent restart
from renewing grace; missing anchors fail closed unchanged. Recovery does not
automatically release a hold. Metadata and root snapshots remain separately aged.

Fixed monitor workers sample statfs, resources and directory inventory outside
the state mutex. Cached admission/reporting never waits for those filesystem
calls. A single bounded persistence worker performs writes outside the mutex;
timeout blocks subsequent admission/reporting and retains its lifetime file lock
until the actual writer exits, without creating replacement workers. Per-seat
start/stop ordering and independent reserve/enforcement remain. A stuck kernel
call can retain its fixed worker and delay joining shutdown: no kernel preemption,
external-watchdog, strict disk-usage or unconditional outage guarantee is made.

SOURCE-PINS proves all seven key A2/A3 implementation/Hub/UI files unchanged.
`softquota.go` adds only the independent threshold/helper and guarded inode hold;
its unknown-age predicates and sampling/writer-gate logic are unchanged. The old no-inode test is
updated to exercise I admission at the emergency equality boundary, consistent
with the new ruling; original baseline tests remain byte-identical. The final test-only child corrects callback error completion: failed assertions return an error to the enforcer instead of abandoning its result channel. All assertions remain, and early suite-timeout experiments are excluded from counted red controls.

## Fresh verification on f713418d

| Fresh gate | Result | Raw log |
| --- | --- | --- |
| Complete canonical, disposable PG parent+5 | rc0,107.897s,python48/shell2 | raw/canonical-inode-final.log |
| nodeagent full | rc0,142/no skips,wall8.003s/package5.741s | raw/nodeagent-inode-final.log |
| nodeagent full race | rc0,142/no skips,wall14.825s/package10.949s | raw/nodeagent-race-inode-final.log |
| Hub emergency reasons and unknown warnings | rc0,2 top-level,16.649s | raw/hosted-inode-reasons-final.log |
| Restored race / Hub / Console | rc0; detailed timing in RESULTS.json | raw/restored-inode-*.log |

The exact executions and timings are in RESULTS.json and the adjacent raw logs.
Full canonical includes the disposable local PG parent and all five subcases;
nodeagent full and race runs contain142 top-level tests with no skips. The
original100 nodeagent tests and33 baseline Go test files remain byte-identical.
A targeted Hub run covers inode/byte reason receipts plus first-warning semantics.
No gate from b807/b92 is relabelled as an f713 run; earlier four-package268 and
browser15/15/8/12 readings retain their earlier pins and unchanged-file scope.
The original two inherited hosted legacy browser failures remain disclosed.

The independent inode cases use byte percent1 and inode percent75, actual inode
total10million and I100000. Free inodes74999 holds/stops/persists/retains data;
75000 and75001 do not emergency-stop, while all remain below I and reject new
writers. Separate configuration tests cover default50, valid1/37/99, invalid
0/100/negative/nonnumeric/overflow, revision binding, actual-total rounding and
odd fixed reserve. Recovery+restart preserves the durable inode hold.

## Current-source withdrawals

The33 Go source controls in raw/INODE-WITHDRAWALS.json include nine independent
inode controls: remove the emergency, change `<` to `<=`, share byte percent,
mislabel the reason, alter the default, remove range checking, disable the env
key, omit policy-revision binding, and remove durable intent before stop.
The retained I-admission mutation also turns red at the equality-only test.
The other controls rerun A2/A3 unknown/age/notification/IO/budget/scheduling and
byte-emergency protections on this same source. One current browser control
removes unknown-specific text. Every counted control is an unchanged-test
assertion rc1; no compiler failure, panic or suite timeout counts. Sources are
restored between controls and restored nodeagent-race/Hub/browser runs are green.

This replaces the obsolete negative that treated all inode emergency behavior
as forbidden. It is not a sum of old26/39/14 control totals. Fixtures use non-Go
suffixes and expose only local synthetic state, fake Engine endpoints and hashes.

## Boundaries and evidence packaging

Only local fixtures/browser/fake Engine servers and expressly required disposable
pgtest are used. No host, real Docker, external PG, real key, Agent, firewall,
artifact/image/release build, offer/job, sandbox, pilot or deployment is touched.
The runner does not inherit external PG DSNs or real credentials. Section7 root
trust/network/owner gates and the four-seat dataconn freeze remain unchanged.
Private fixture/process/cache cleanup is in CLEANUP.json; shared caches and other
seats are untouched.

CURRENT.md points here. The older follow-up manifests update only the current
pointer and its containing manifest hashes, so immutable historical reports/logs
keep their attribution while all relative-path manifests remain verifiable in
the delivered checkout. New source pins bind the ruling object, actual five-file
increment and complete baseline delta. Formal review remains with opus50ts.

Final cleanup: owned fixture processes0, mutation checkout removed; private cache678,713,392 bytes/HOME83,357,075 bytes/TMPDIR1,140,936 bytes removed. Shared caches/other seats remain untouched.
