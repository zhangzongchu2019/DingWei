# A1/A2/A3 + N1/N2 combined successor

Repository: `git@git.code.tencent.com:wego-ai/dingwei.git`
Ref: `review/coder/node-soft-quota-impl`
Baseline r21: `8944a7c12198129af74fbe9c7ad6d646ad5c5d24`
Actual code/test pin: `0e5bb86d2b81956fdb6dd79e839804e3f834a4bc`
Code tree: `363a17979fd280f43cf0a61169793eede8734b6b`
Reviewer: opus50ts. This is author delivery, not independent PASS or execution GO.

## Scope and authority

The latest direct leader instruction requires both N1 (production administrator
CLI registration) and N2 (actual login writers) together with A1 in this delivery.
It supersedes the earlier E/N2 deferral. A1, A2 and A3 remain implemented; B capacity
rules remain a separate next increment after this candidate PASS. Earlier reports
and logs keep their historical source/scope attribution. No history is rewritten.

`f3e6448d` combines A1/A2/A3 and N1. This successor adds N2 through `cf76c473`
and the ordinary-seat retirement guard in `0e5bb86d`. SOURCE-PINS binds every
non-document increment, all33 original baseline Go test files, and unchanged
helper/renderer/lock/config/CLI component identities.

## Retained A1/A2/A3 and actual CLI N1

A1 is leader-added scope: independent inode emergency percentage, default50,
range1..99, revision-bound; the original I admission gate remains. Fresh free
inodes strictly below the independently calculated emergency line hold and stop
managed writers without deleting data. Byte emergency and inode emergency are
independent. Equality alone does not trigger emergency, but can deny admission.

A2 first statfs unknown refuses new writers and warns; it is not treated as zero
free space. Beyond the persistent last-good/initial age anchor (default60s),
unknown causes hold/stop. Recovery never automatically clears a hold. A3 fixed
workers sample usage/statfs/resources/inventory outside the state mutex; admission
and report use cached snapshots. Private persistence and writer/stop ordering
remain bounded and fail closed. Kernel-stuck filesystem IO is not preempted and
may retain its one fixed worker/delay shutdown; no external-watchdog or hard
maximum-overrun/outage guarantee is made.

N1 remains the actual `hosted-admin register-node --quota-mode` and
`--quota-policy-revision` path, feeding `RegisterNode -> normalize -> stored
labels/audit`. Default hard, explicit soft, lowercase64-hex revision and labels-only
activation refusal remain. Tests invoke real CLI shape and real normalization
against disposable loopback PostgreSQL; no direct-SQL substitute is claimed.
Existing real admin initialization opens/configures its database before flag
parsing; this is not a read-only production preflight.

## N2: registered login writers, budget and stop ordering

`loginOps.start` now performs cached admission even before replay/image handling,
then reserves a typed `login-<32hex>` record before workspace/network creation.
Its own pool is bounded to2 including retained records; ordinary MaxSeats still
counts ordinary seats. This does not implement B's future capacity policy.

Each login reserves its actual fixed CPU hard quota250m and memory256MiB, plus a
conservative256MiB disk commitment derived from the same memory limit (the private
login workspace is normally tmpfs). These reservations share the existing node
CPU/memory/disk budget with seats and remain charged while held. tmpfs/shm memory
is inside the container memory ceiling; this accounting is conservative and is
not a new guarantee of physical root-disk enforcement. All node thresholds remain
policy-configured. UID10001/read-only rootfs/CPU/memory/PID/no-stdout-log existing
login specifications remain; soft adds its node ownership label.

After Engine creation the actual CID is persisted before attach/start. Login
start goes through the same measurement/admission and per-writer gate as other
writers. Root-byte/inode emergency and expired unknown enumerate these records
and use the same bounded enforcer. Before stop, live inspect must match recorded
CID, approved node label, login ID and the exact private `/h` bind. Wrong ownership
or mount refuses stop. No generic unlabelled container is adopted. An in-flight
start and stop serialize; a late start during hold is not reported successful
and is followed by the held writer's stop. No automatic restart/retry/unhold.

Usage measurement is metadata-only, using the existing dual allocated/apparent
walker, hardlink deduplication, sparse-file and no-follow rules on the login's
private root. It does not read auth contents. Private state retains the workspace
path for restart/reconciliation; capacity strips path and container IDs and adds
`writer_kind=login`. Typed events are audited/idempotently acknowledged. The Hub
never applies a login event or login capacity entry as a hosted-seat state update,
even if an artificial seat ID collides. No authentication route is added.

## Hold, privacy and retirement

Emergency hold takes precedence over normal login cleanup. `finish` suppresses a
new credential read/upload when it observes hold; cancel and startup cleanup
retain held workspace/container/network and charged resources. A request already
authorized/in flight is not claimed atomically cancelled by this change. The
normal successful/cancelled flow still uses the original privacy cleanup and
releases budget only after confirmed container removal. Retirement serializes
with starts/stops; data removal occurs only once no writer remains and no hold
was present. An emergency arriving during removal preserves the directory.
Ordinary seat teardown cannot forget a login writer; only typed, confirmed
retirement can release its record.

Clean controller shutdown persists a hold for any remaining live login; unclean
restart also holds records. Held state is not silently reset, migrated or deleted.
Interrupted normal retirement is reconciled, including when the directory is
already absent. Unknown preexisting login objects or a changed configured root
fence admission rather than being silently imported/deleted. The feature requires
operator reconciliation of legacy unrecorded objects before use.

Unlike ordinary immediate privacy cleanup, held login state may retain synthetic
(or in production, real) login material in its private workspace. No contents are
included in state/report/audit or exported by this implementation. Incident
resolution/deletion requires the owner's separate decision; cancel is not an
unhold/destructive recovery API. Default `/dev/shm` preservation is process/container
retention, not durability across host reboot or tmpfs loss. No hot downgrade to a
pre-N2 controller is approved: old code cannot enforce these typed records;
stop/reconcile all writers and preserve paired compatible private state first.

## Verification and limits

Fresh exact-source gates, timings, counts, log hashes and restored controls are
in RESULTS.json and raw/.

| Gate | Source | Result | Wall seconds |
| --- | --- | --- | --- |
| Full canonical, disposable PG parent+5 | 0e5bb86d | rc0 Python48/shell2 | 104.304 |
| nodeagent full | 0e5bb86d | rc0 152/no skips | 12.097 |
| nodeagent full race | 0e5bb86d | rc0 152/no skips | 16.737 |
| Restored targets across three packages | 0e5bb86d | rc0 16/no skips | 10.078 |
| Hosted-admin scenarios | cf76c473 | rc0 4/no skips | 77.537 |
| Hosted full | cf76c473 | rc0 150/no skips | 472.356 |

 All original100 baseline nodeagent cases and33 original
Go test files are unchanged. The current nodeagent suite has152 top-level passes.
The canonical gate includes the disposable PostgreSQL parent and all5 subcases;
no PG exclusion wrapper or external DSN is used.

Eleven fresh unchanged-assertion source controls cover: A1 emergency/independent
percentage; N1 old CLI/missing revision; actual old login path (Free=1 does not stop
loginCID); stop ownership routing; login resource reservation; held cleanup;
typed audit/typed capacity; and ordinary teardown forgetting a login. Every red
must be an actual assertion failure, not compile error, panic or suite timeout.
After restoration the corresponding targets are green. Earlier A2/A3 and wider
controls keep their original pins; they are not added to this current count.

The extra full hosted-package/CLI and early targeted runs used `cf76c473`; their
unchanged component identities are recorded explicitly. They are not relabelled
as an entire-tree run on `0e5bb86d`. Fresh required canonical/nodeagent/race and
restored target runs are actually on `0e5bb86d`.

Local synthetic HTTP/TLS Engine/Hub fixtures and disposable loopback PostgreSQL
only. No actual Docker daemon/image build, host access, external/production PG,
real keys/accounts/Agents, firewall, job/offer or deployment. No full helper or
blanket browser PASS. Thresholds/trust/network/owner gates and dataconn freeze
remain. Cleanup evidence records only this seat's temporary paths/processes.

Final cleanup: no marker-matched owned fixture processes; private Go cache
968,390,357 bytes, HOME83,050,377 bytes, TMPDIR1,139,530 bytes and clean mutation
checkout removed. Shared module cache/other seats and author worktree retained.
