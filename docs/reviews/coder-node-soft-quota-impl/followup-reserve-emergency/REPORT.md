# Reserve independence and emergency-stop repair — author delivery

This successor replaces e2e5cf54 as the implementation delivery. That candidate
missed the two mandatory product changes; its 39 controls/125-test readings are
historical, not acceptance of these changes. No independent review PASS is claimed.

Repository: `git@git.code.tencent.com:wego-ai/dingwei.git`.
Ref: `review/coder/node-soft-quota-impl`.
Baseline remains r21 `8944a7c12198129af74fbe9c7ad6d646ad5c5d24`.
Actual repaired code/test pin: `8a3efd78d675f74bff9cf1974bd6d17c0c0a2c62`,
tree `5e71c65100aa7df7879c5c00063ed304ca2fe2ff`. Later evidence commits are docs-only.
Review owner: opus50ts. Promised revised delivery ETA: 2026-10-17T10:00:00Z.

## Independent reserve path

`runSoft` now has three bounded workers: one scan worker, one reserve loop and one
enforcer. No extra long-lived workers are created per tick; bounded stop batches still
fan out over recorded targets. One buffered signal coalesces pending
enforcement. Both periodic loops run initially, independently of Hub connectivity.
Cancellation joins the workers. Scan metadata IO and Engine IO hold no quota-state
mutex. State snapshots are revalidated by seat identity before merging; a newer
writer measurement is not overwritten by an old scan.

Starting a writer and stopping it share a per-seat, context-aware operation gate. It is acquired after the fresh
walk, so a blocked walk does not obstruct stopping an existing writer, and one
seat's blocked start cannot obstruct stopping an unrelated seat.
Reserve can set/persist a hold while a walk or Engine start is in flight. A late
start is returned as a safety failure, and the automatic stop waits behind that
start rather than inspecting an as-yet-stopped container and permitting it to
start afterwards. Engine stop batches serialize separately without occupying the
state mutex. A request is `pending` until completion; failure is `unknown`, never
falsely `stopped`. Final admission/hold/recorded-container checks remain enforced.

This is process-level independence, not an independent host watchdog or kernel IO
preemption guarantee. Filesystem metadata reads, statfs, state fsync and a failed
kernel/Engine can still stall. No strict maximum outage or disk overrun is claimed.
Existing trust/network/owner/sandbox gates remain required.

## Explicit emergency line

`NodePolicy.EmergencyReservePercent`, configured by
`NODEAGENT_EMERGENCY_RESERVE_PERCENT`, defaults to **50** and accepts only 1..99.
It is included in the approved policy revision; an invalid value stops startup.
Both R and this percentage are operator node configuration. The effective R is:

- bytes: max(configured fixed reserve, configured filesystem percentage);
- inodes: max(configured fixed inode reserve, configured filesystem percentage).

The emergency threshold is ceil(R × configured emergency percentage / 100), for
each dimension independently. Thus the default is R/2, rounded up so it cannot
become zero. Below either line, the reserve loop immediately puts **all registered
managed seats** into durable, non-auto-clearing holds, then signals enforcement.
It stops verified recorded containers and retains identity, accounts and data.
At equality or above there is no emergency hold, but being below ordinary R still
rejects writer admission. Recovered free space and a normal restart do not release
existing holds. There is no state reset, migration, deletion or release endpoint.

Unknown/invalid root space is treated conservatively as `node_space_unknown` and
also holds managed seats. Byte/inode emergencies have distinct audit reasons
`node_space_emergency` and `node_inode_emergency`. Hub event validation accepts
these reasons through the same authenticated, idempotent transaction/ACK path;
no alternate credentials, routing or event acknowledgment mechanism is added.

The defaults are proposed headroom, not owner-approved production sizing. They do
not resist untrusted host root or constrain writes before the next check. No host
firewall, standalone daemon, service installation, real Docker or host operation
is included. Hard remains default; the new monitor is entered only in soft mode.
CPU/PID/log contracts and existing hard/XFS behavior are retained.

## Fresh execution on the repaired pin

| Check | Actual result | Log |
| --- | --- | --- |
| Complete canonical incl. disposable local PG | rc0,107.369s; python48/shell2; PG parent+5 passed | raw/canonical-reserve-final.log |
| nodeagent full | rc0,132 top-level passes/no skips; package6.015s/wall7.482s | raw/nodeagent-full-reserve-final.log |
| nodeagent race full | rc0,132/no skips; package10.905s/wall11.613s | raw/nodeagent-race-reserve-final.log |
| Seven new reserve/emergency/writer targeted cases, race ×10 | rc0; all70 top-level executions passed | raw/perseat-writer-targets.log |
| Related hosted/nodegate/cmd-nodeagent/workpulse full | rc0,267 top-level passes/no skips; wall398.902s | raw/related-packages-reserve-final.log |
| Restored isolated withdrawal checkout, all affected targets | rc0,8.452s | raw/reserve-final-restored.log |

The seven-case targeted command was run immediately before committing the
same relevant source/test bytes;
SOURCE-PINS binds them to the committed code. The full canonical/full package
commands were started after that commit. Original100 nodeagent tests and all33
baseline Go test files across nodeagent/hosted/nodegate remain byte-identical.
The previous UI/browser readings keep their actual source/pin attribution: UI
bytes are unchanged by this repair. In particular, the two inherited browser
failures are still disclosed, not relabelled green. No full helper claim is made.

## Current source withdrawals

`raw/RESERVE-WITHDRAWALS.json` records **14 current-source controls**, all actual
assertion rc1 with unchanged target tests, no compiler or suite-timeout red:
serial scan/reserve, serial stop/reserve, state lock held during scan, removal of
start/stop ordering, byte emergency, inode emergency, immediate durable reserve
hold, scan budget, whole-batch stop budget, later periodic scan, emergency default,
Hub emergency-reason acceptance, a node-global writer gate, and writer-gate
acquisition before the metadata walk. Original files are restored between controls.
The three budget/periodic controls have been retargeted to the repaired functions.

The earlier e5958bf precursor used a broader inode mutation, retained as a
separately named historical control. It is excluded from the14. All counted
controls here run on8a3efd78; the inode mutation touches `checkReserve` alone. Earlier controls
from e2e5cf54 are historical and are not summed into a new current-source count.

Fixtures demonstrate reserve activity while the metadata walk remains blocked,
and several later reserve ticks while an Engine stop remains blocked. Boundary
cases test bytes/inodes just below, exactly at and just above the emergency line;
they verify durable intent before enforcement, data retention and hold retention
after recovery/restart. The late-start case checks no premature stop inspection
and a failed start result followed by an actual stop in the fake Engine. A
separate two-seat case proves that a blocked start cannot serialize unrelated
seat stops. All7 new top-level tests were repeated10 times with race enabled.

## Execution boundary and cleanup

Only local Go/Python/browser fixtures, fake Engine HTTP servers and explicitly
required disposable loopback pgtest were used. The runner uses a whitelisted
environment/private HOME/cache/TMPDIR and does not inherit external PG DSNs or
real credentials. No host, real Docker, real Agent, key, firewall, artifact/image
build, offer, sandbox, pilot or deployment action occurred. Dataconn freeze and
owner authorization gates remain unchanged. Final cleanup is in CLEANUP.json;
only this seat's private fixtures/cache are removed, with shared caches untouched.

Final cleanup: zero owned fixture processes; restored mutation checkout removed.
Private cache801,986,811 bytes, private HOME83,050,377 bytes and disposable
TMPDIR1,139,530 bytes were removed. Shared caches/other seats were untouched.
The repository-relative manifest covers this follow-up directory and CURRENT.md;
original delivery manifests retain their original historical scope.

Extra raw `*-reserve-fix.log` and `reserve-withdraw-restored.log` files are
preceding e5958bf runs, retained as history only. They are not the repaired final
pin executions in the table above, whose log names use `reserve-final`/`final-restored`.
