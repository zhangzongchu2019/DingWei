# S-NODE-SOFT-QUOTA — author delivery (review pending)

Repository `git@git.code.tencent.com:wego-ai/dingwei.git`, branch
`review/coder/node-soft-quota-impl`; baseline r21
`8944a7c12198129af74fbe9c7ad6d646ad5c5d24`.
Final code/test pin `0d43a75ea7d3dc025edeb5e7a414ab00e12a1bf8`, tree
`f1e54ab3cdf63feea7565db3193821e2b238f101`. Subsequent delivery changes only docs.
Review owner: opus50ts. No self-issued independent PASS or execution GO.

Implementation and defaults are described in [IMPLEMENTATION.md](IMPLEMENTATION.md).
Hard remains the default with the original XFS implementation; soft is explicit,
registered/revision-bound, user-consented and visible in Console/audit. Measurement
uses metadata-only allocated/apparent accounting; durable holds stop recorded,
label/bind-verified containers without deleting data. All writer paths, replay,
node reserve and resource parameters have local positive/negative controls.
CPU/PID/log bounds are added without modifying existing containers or Engines.
No helper runtime, version pins, requirements lock or Go dependencies changed.

## Mandatory gates and exact attribution

| Check | Execution pin / result | Raw evidence |
| --- | --- | --- |
| Complete canonical, including disposable pgtest | 0d43a75: rc0,137.682s; python48 + shell2; PG parent and all5 subcases passed | raw/canonical-0d43a75.log |
| Full nodeagent | 0d43a75: rc0, package4.811s/wall5.293s, 125 top-level tests, zero skips | raw/nodeagent-full-0d43a75.log |
| Full nodeagent race | 0d43a75: rc0, package9.304s/wall31.315s, 125 top-level tests, zero skips | raw/nodeagent-race-0d43a75.log |
| Metadata FD child | GCoff/RLIMIT128, 1000 traversals, FD7→7; only child process changes its limits | same full logs / raw/durable-and-fd.log |
| Related hosted/nodegate/cmd-nodeagent/workpulse full packages | 0efc288: rc0, 266 top-level tests, no skips, wall395.214s | raw/related-packages-0efc288.log |
| Final hosted policy/contract targets | fc3db12: rc0, wall10.750s | raw/hosted-soft-final-targets.log |
| New real Console DOM | fc3db12 restored checkout: rc0,10 assertions | raw/extra-withdraw-restored-browser.log |

The four related source directories are byte-identical between the executed
0efc288 and final 0d43a75; SOURCE-PINS records this check. Later M8 HTML changed
(default body compatibility) and nodeagent tests were appended, so the old related
full gate is attributed to its actual pin, **not** labelled a fresh full-suite run
of the final whole tree. Final canonical rebuilds/tests current M8; full nodeagent
and race execute the final test set. All100 baseline nodeagent tests and all33
existing test files across nodeagent/hosted/nodegate are byte-identical to r21.
Hard options JSON and default create-body shapes were preserved in product code;
original tests were not relaxed.

## Source withdrawals and restored controls

`raw/WITHDRAWALS.json`: 29 isolated source withdrawals, pin726216b; source bytes
for every mutated file match final 0d43a75 (recorded SHA256). Covers mode/trust,
entry/replay/final hold gates, disk/inode/commitments/seat count/CPU/memory budgets,
unknown/stale usage, warning hysteresis, pre-stop durable hold/atomic replacement,
container ID/label/bind ownership, hard CPU/PID/log limits, policy binding,
hard priority, user consent and default-socket aliases.

`raw/EXTRA-WITHDRAWALS.json`:7 more actual source withdrawals at exact fc3db12:
registered replay/backend/monitor hooks and four Console protections (notice,
eligibility, consent, held-row notice). These36 are actual assertion rc1, not
compiler failures, unchanged expectations. Original sources were restored between
mutants. Restored full nodeagent124, registered-handler target and new DOM suite
all rc0. Final code/test pin0d43a75 adds a periodic measurement test plus parent-deadline controls to the new budget tests; full/race125 are green. The isolated worktree was verified clean before disposal.

`raw/BUDGET-WITHDRAWALS.json` adds3 actual source withdrawals at0d43a75:
scan deadline, whole stop-batch deadline and periodic scan invocation. Each returns
rc1 at the original target assertion (not a suite timeout/compiler failure), then
the source is restored. Together **39 valid source-withdrawal controls are red**.
Fixed budget/periodic targets pass; the final full125/race125 pass on the restored
author code. The budget negatives have a separate two-second parent deadline to
finish safely after withdrawal without turning a suite panic into a red claim.

Two initial mutation controls produced compiler errors from unused variables;
they are explicitly retained as invalid controls in WITHDRAWALS-FIRST and
`*-invalid-build-control*` logs and excluded from39. Corrected controls preserve
variable references and die at the intended test assertion. No author worktree
or original test expectation was changed to manufacture a red result.

## Browser boundary and retained baseline failures

Existing model8, lifecycle15 and quota15 browser cases pass unchanged. The old
`console-agent-mgmt-hosted-regression.js` reports **30 PASS / 2 FAIL** both on
original r21 and the candidate's final UI bytes (08dd27c UI equals final0d43a75):

1. Its left-list test expects exactly `/view/leader.zzc`, while existing production
   adds `view_refresh_instance`; it fails before returning to the group pane.
2. The later new-group create case consequently runs with that pane hidden and
   times out. Exact failure names match the baseline control.

Raw baseline/candidate logs are retained. This is not an all-browser-green claim,
and those old assertions were not edited. The additional false-consent body-field
regression observed initially was fixed in product: unchecked sends the original
body with no `allow_soft` field, checked explicitly sends true. That old success
case now passes; it is not counted among inherited failures.

## Corrections and non-results

Historical logs remain historical: initial851fc5a had a generated file overwritten
with the wrong package (fixed by0efc288); the first canonical launcher raced with
its PATH update and selected system Python without locked metadata. Subsequent
runner configuration was frozen first, provenance verifies the existing locked
Python3.12.3 with websockets15.0.1/pexpect4.9.0/ptyprocess0.7.0/certifi2026.7.22.
Other initial fixture-only failures (long Unix path and nonprivate TempDir) were
fixed in fixtures, without weakening production privacy checks. Final result
files name the actual commands and pins; preliminary rc1 is never relabelled.

No real host, Docker daemon, external PG, credentials, image/release build,
firewall/cgroup installation, offer, Agent operation or deployment occurred.
Only fake Engine APIs, loopback fixtures and disposable local pgtest were used;
the test environment whitelist does not inherit external DSNs or real credential
environment. Default resources/thresholds are proposed conservative values, not
owner-approved production sizing.

## Remaining operational gates

This delivery does not implement or install host firewall/relay rules or an
independent host watchdog. Approval references are operator records, not proof
that those controls exist. Old RW Docker socket holders remain host-root
-equivalent; shared-kernel/root disk/IO risks and bilateral IPv4/IPv6 isolation
must be accepted/qualified per approved design§7. Soft has no finite worst-case
disk overrun guarantee. Service UID10001/chown, actual kernel cgroup enforcement,
image/CLI compatibility, filesystem scan/stop latency and crash recovery still
need separately authorized same-version sandbox and owner decisions.

Holds deliberately have no automatic release; release authority was left to the
owner in the design. No unauthorized release endpoint or state reset was added.
A separately approved release/recovery workflow and independent monitor-outage
protection are required before a real pilot. Console/audit notification is local
visibility/durability only; no external chat/email delivery is claimed. Dataconn
four-seat freeze and all host/credential/sandbox/pilot gates remain unchanged.

## Cleanup

Final private Go cache329,599,938 bytes, private HOME83,050,377 bytes and
disposable TMPDIR569,765 bytes were removed after gates; zero owned fixture
processes remained. The restored detached mutation worktree was clean and removed.
The earlier cleanup record is retained separately as CLEANUP-FIRST.json; caches
were recreated only for the final periodic/budget controls and gates, then removed
again. No shared module cache, other seat or live state was changed.
