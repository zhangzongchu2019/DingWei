# S-NODE-SOFT-QUOTA implementation

Baseline: `integration/spec5ff-r21` `8944a7c12198129af74fbe9c7ad6d646ad5c5d24`.
Approved design: `review/coder/node-soft-quota` `40467c45d26b125f15970ac17a6b8ef9fdd7cfbf`,
review `d472e997f1e56b19d871ad9feaaf0de7a72d0c7e`. This is code/local-test work,
not installation, host/network change, image build, key handling or pilot approval.

## Contracts

* `hard` remains the default and still calls the existing XFS implementation.
  There is no automatic fallback and no zero-limit bypass for soft. Existing hard
  tests are retained verbatim. Legacy hard options JSON keeps its original key
  set; soft options add mode, eligibility and `无硬磁盘配额` notice. New capacity
  frames explicitly report mode and policy revision. CPU/PID/log bounds apply to
  new containers in both modes; existing containers/Engines are not modified.
* Soft registration is administrator configuration (`Node.QuotaMode`,
  `QuotaPolicyRevision`, administrator label `allow_soft=true`). The revision is
  SHA256 of the node policy JSON, including thresholds and approval references.
  `nodeagent --print-policy-revision` reads configuration only and exits before
  credentials, ledger or Engine access. An incoming frame cannot change the
  registered mode. A missing/mismatched/unready/stale soft report is ineligible.
  User `allow_soft=true` is separately required for manual and automatic soft
  selection; eligible hard nodes are preferred after pool/model/memory filtering.
  Consent is audited in the seat creation transaction. Legacy hard nodes receive
  the original command shape; explicit CPU millicores are sent only after the new
  resource capability is advertised. New agents also cap legacy plan-share input.
* Soft state is `NODEAGENT_LEDGER_FILE.soft-quota`, outside seat root, in a private
  directory; its regular 0600 owner-matching single-link state/lock files are not
  placed in `/work`. One controller owns a nonblocking lifetime lock. State writes
  use file sync, rename and directory sync. Unknown/corrupt/aliased state is never
  reset. State binds node ID and absolute seat root. Existing unregistered seat
  directories prevent admission; there is no adoption or layout migration.
* Measurement reads metadata only through pinned no-follow directory handles.
  Allocated bytes use `st_blocks*512`; apparent bytes include sparse sizes; charge
  is their maximum. A per-seat inode set deduplicates hardlinks, while each seat
  independently pays for cross-seat aliases. Symlinks are not followed, FIFOs are
  not read. Cross-device and mount-table nested mounts (including same-device
  binds), depth/count/overflow/read errors become unknown, not zero. The shared
  round budget defaults to 5s, period 30s. Deadline is checked during traversal
  and again before accepting a result. A blocked kernel filesystem syscall is
  not preemptible by a Go context: no strict wall-clock/disk bound is claimed.
* 80% warning has 75% recovery hysteresis; 100% of the positive plan limit sets a
  durable hold before stopping. Scan failures block new writers immediately;
  stale usage at 60s produces a hold. The monitor runs independently of Hub WSS;
  a five-second reserve/staleness tick retries enforcement. Stops share one batch
  timeout, not timeout multiplied by seat count. Engine ID, recorded container
  ID, owner/seat labels and exact `/work` binding identify stop targets. Stop is
  confirmed by inspect; failure remains unknown. No monitor path calls Clear,
  Destroy, Retire or exports business data. A soft partial create failure also
  preserves its directory/identity for reconciliation.
* All create/start/restart/upgrade/refresh paths and the final writer start gate
  check policy; cached successful start results are checked before ledger replay.
  Refresh refuses an already held seat before touching the credential file.
  Upgrade `-new` IDs are durably recorded before starting; prior IDs are untracked
  only after removal. Login and image import also check node reserve. Concurrent
  creates reserve commitments under the controller mutex. Stopped seats retain
  disk/CPU/memory commitments until explicit destruction. Disk admission uses
  current free bytes minus max(fixed, proportional) reserve, unallocated seat
  commitments and image-import reserve; inode reserve is independent. The current
  implementation requires seat and isolated Engine data roots on one filesystem.
* Holds never auto-clear, including after shrinking data or restarting. An
  unclean controller exit conservatively produces `unclean_restart` holds on
  the next boot (the active marker protects against an unpersisted ENOSPC hold).
  This increment deliberately exposes no hold-release endpoint: release authority
  was left for the owner in the approved design. A future separately reviewed
  release workflow must verify stopped writers, fresh below-recovery readings,
  capacity and actor/approval, and audit before any separate restart. Do not edit
  state files or reset identity/journal to bypass holds. This operational gate is
  required before a real pilot; this is not a claim of a complete recovery pilot.
* Warning/hold metadata is visible in the Console row and persistent audit.
  A bounded durable 256-event outbox is acknowledged only after the authenticated
  Hub transaction commits; repeated receipts do not duplicate audit. Overflow or
  private persistence failure blocks admission and conservatively stops managed
  seats. It never drops an unacknowledged event to keep serving. No email/SMS/
  external chat sender is added. Hub/UI distinguish safety failure from success,
  preserve held identity/accounts and do not pretend a failed stop is confirmed.

## Defaults and reasons (owner must choose final values)

The complete non-secret node keys are in `deploy/nodeagent.env.example`. Invalid
or zero disabling values fail startup. Defaults deliberately leave headroom:

| Setting | Default | Reason |
| --- | --- | --- |
| mode | hard | existing XFS policy, no silent downgrade |
| scan/budget/max-age | 30s / 5s / 60s | limit scan work, reject unknown immediately, stop stale writers |
| reserve tick / batch stop budget | 5s / 10s | bound admission/confirmation latency without per-seat multiplication |
| warning/recovery | 80% / 75% | early warning and hysteresis |
| disk stop | 100% positive plan DiskMB | preserve the plan's upper limit, never disable by zero |
| bytes | max(32GiB,10% filesystem) | shared-root emergency/headroom; not a write-rate guarantee |
| inodes | max(65536,1% filesystem) | avoid small-file exhaustion independent of bytes |
| image import reserve | 8GiB | provisional expanded-image headroom, must be qualified against pinned images |
| max seats | 4 | conservative initial scan/fanout cap, not proof of capacity |
| CPU / memory host reserve | 2000 millicores / 4GiB | preserve node/old-workload headroom; unbounded old actors can still exhaust it |
| seat/login PIDs | 512 / 128 | bounded process fanout, pending real CLI qualification |
| CPU period | 100000us | fixed unit conversion from explicit plan CPU (legacy plan shares fallback) |
| logs | local 10MiB × 3 | bound new seat logs; login stays `none`, never device-code logs |
| rootfs / tmpfs / shm | readonly / 8MiB / 64MiB | maintain rootfs/tmpfs contract; explicit default shm bound |

Hard quotas remain XFS, not emulated by measurement. Soft cannot prevent writes
between scans; absent a proved bandwidth limit, worst-case overrun can consume
the entire shared root disk. Unbounded legacy memory/PID/IO consumers remain
outside the new seat limits. No finite maximum overshoot or downtime is promised.

## Trust, network, rollout boundaries

Soft startup requires non-secret risk, host-root trust and network approval
references plus an approved isolated Engine ID/root and existing nondefault Unix
socket; aliases to the default socket are rejected before API inspection. Engine
preflight checks identity/root and advertised cgroup-v2/systemd memory/swap/CPU/PID
support without changing the daemon. It is not an enforcement benchmark.

These strings are operator records, NOT proof of owner consent or deployed
network isolation. Design §7 remains docs/config only: no firewall, route, UFW,
SSH, old container/socket permission, old Engine config or host cgroup writes are
implemented. On host 43, prodops' attributed 06:55Z readings identified three host
network containers, two with RW old Docker sockets. Those actors are host-root
-equivalent: separate daemon, 0700, Engine ID and cgroups cannot resist them.
Their acceptance (or an independent trust-domain host/VM), bilateral IPv4/IPv6
host-origin/seat/relay/management rules, rollback and same-version sandbox tests
remain explicit owner/prodops gates. This code/local testing does not verify them.

No standalone host watchdog or service installation is performed here. Monitoring
is independent of Hub connectivity, but process/kernel/Engine failure still needs
an independently approved watchdog/recovery deployment and real fault tests.
`Restart=always` is not a claim that live writers are protected during a prolonged
monitor outage. No real host/Engine, cgroup enforcement, du throughput, CLI
compatibility, filesystem stop latency, notification delivery or pilot is claimed.

## Local evidence

Raw results, exact source pins and actual source-withdrawal outcomes are delivered
with the evidence commit after final gates. Preliminary failures are retained and
not relabelled as final PASS. Complete canonical includes disposable pgtest;
external DSN/proxy/credential environment is not inherited by the test runner.
