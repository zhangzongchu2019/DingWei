# Current delivery: N1 hosted-admin soft registration, final ruling E

Repository `git@git.code.tencent.com:wego-ai/dingwei.git`; ref
`review/coder/node-soft-quota-impl`. Review owner: opus50ts. This is an author
implementation delivery; independent review is pending, with no deployment GO.

Actual code/test pin `4d2f408441bd867dc6cdb64ad21928c0ed2c468c`,
tree `387fc71ec9089cf750a62df5e56870be58f34823`.
Baseline remains r21 `8944a7c12198129af74fbe9c7ad6d646ad5c5d24`.
The precise predecessor for the final review is17115394397412d92be660df9c2cc2f1865c23a6
(code b8074831). Later delivery commits contain evidence only.

## Current scope authority

The explicitly requested latest rulings branch was fetched at
`review/opus50tc2k/rulings` exact `11d649934352c0cbcbc5efa7b2d1b38bbb2cefd8`,
tree `eca070105f5874a811b3f5ab2b5fe4823e82c913`,
`docs/rulings/S-NODE-SOFT-QUOTA-20261017.md` **E (final)**. It supersedes C/D:
only N1 is repaired here; A1 and N2 are accepted deferred follow-ups. The earlier
inode implementation is retained in Git history but explicitly reverted from
this candidate in682bff6. No force push, history rewrite or host state change.

Relative to17115394, the complete non-document delta is exactly TWO files:
`cmd/workpulse/hosted_admin.go` and new `hosted_admin_quota_test.go`.
SOURCE-PINS proves nodeagent/hosted/nodegate/m8/helper and node config byte-identical
to17115394, preserving A2/A3/I. Existing admin tests are also unchanged.
There is no inode emergency percentage/monitor in this final candidate.

## N1 behavior

Actual `workpulse hosted-admin register-node` now accepts:

- `--quota-mode`: default `hard`; explicit `soft` to register a soft node;
- `--quota-policy-revision`: soft requires the approved64-lowercase-hex SHA256.

Both flags are forwarded as hosted.Node fields to the existing RegisterNode
service. Its existing normalizeQuotaRegistration validates mode/revision and
normalizes labels before node/credential/audit writes. Supplying a soft label
alone cannot activate soft; malformed/missing revisions are rejected. Successful
registration persists mode/revision in canonical labels, preserves other labels,
and audits `no_hard_disk_quota`; the one-time credential output remains unchanged.
No new endpoint, bypass, direct SQL registration, account-key handling or retry
path is added. Hard/default behavior is preserved. Policy approval and actual
node identity/trust/network prerequisites remain operator/owner gates.

CLI still opens the configured store and runs schema setup before parsing flags,
which is its pre-existing documented behavior. It is NOT a read-only production
preflight. No real invocation or external PG was used for this delivery.

## Verification and current withdrawals

The new test executes the actual CLI via execute -> runHostedAdmin -> RegisterNode
-> normalize -> disposable local PG. It asserts stored labels plus audit, default
hard behavior, mode rejection, missing/bad/uppercase revision rejection, rejection
of labels-only soft activation, no credential stdout and no node/credential rows
on invalid registration. Synthetic fixture credential output is captured and
never copied to reports. It neither starts Hub nor connects to a real node.

Fresh exact-code commands/results/timings are in RESULTS.json and raw logs:
complete canonical48+2 including disposable PG parent+5; nodeagent140 tests;
all existing hosted-admin tests plus the new end-to-end case; and a restored
end-to-end run. No earlier gate is relabelled as a new run.

Three unchanged-test source controls return actual assertion rc1: restoring the
old CLI, removing mode forwarding, and removing revision forwarding. There is no
compiler/suite-timeout/panic red. Sources are restored between controls, and the
restored end-to-end test returns rc0. The two fixtures have non-Go suffixes.

## Explicit deferred limits and next work

- **S-SOFT-INODE-EMERGENCY (A1):** no inode emergency stop in this candidate.
  The original I admission reserve remains. Separate later scope retains the
  independent default50/range1..99 configuration/test requirement.
- **S-SOFT-LOGIN-WRITER (N2):** short-lived login containers are not fully covered
  by the soft usage/recorded-container hold/stop loop. Existing preflight checks
  do not establish full lifecycle accounting. Owner E accepts this transitional
  limitation; no complete-login-loop claim is made.
- **B seat-capacity rules:** not implemented or started in this delivery. After
  this candidate PASS, open a separate branch for configurable1core/1.5GB/40GB,
  three-dimensional minimum capacity, reserves/unmanaged-load unknown rejection,
  create/start admission, Console reasons and the owner's8/9 example.

A2 first-error warning/admission refusal and bounded last-good expiry, A3 fixed
lock-free samplers/cache, bounded persistence worker, byte R/2 strict comparison,
I reserve, existing hard tests and original design40467c45 remain as in17115394.
Their prior evidence keeps its exact source attribution; no pre-signed PASS is
inferred. Original inherited browser limitations remain disclosed.

## Schedule, boundary and cleanup

N1 delivery ETA2026-10-17T10:00:00Z. B first delivery is estimated
2026-10-17T13:00:00Z only if this candidate PASS arrives by10:00Z; if later, report
a revised absolute UTC. No B branch is opened before PASS.

Only private local fixtures, synthetic test credentials and required disposable
loopback pgtest are used. No real host, Docker, external PG, real key/credential,
Agent, firewall, artifact/release/image build, offer/job, sandbox, pilot or
production change. Owner gates and dataconn four-seat freeze remain unchanged.
CLEANUP.json records owned-process/cache/HOME/TMPDIR/mutation-checkout disposal;
shared caches and other seats are untouched. CURRENT.md points here; earlier
manifest wrapper hashes update only that pointer chain, preserving original
reports/logs as historical executions.

Final cleanup: zero owned fixture processes; cache612,130,323 bytes/HOME83,050,377 bytes/TMPDIR569,765 bytes and restored mutation checkout removed. Shared caches/other seats untouched.
