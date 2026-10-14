# A6 implementation and author evidence

Historical initial fedfd145/51538 evidence below. For the current 30s R1/R2 delta and its actual logs, see ../coder-a6-r1-r2/REVIEW.md. Initial counts/profile are not silently re-labelled as results for the follow-up.

Status: offline implementation candidate, not independent CR, target verification, deployment or owner acceptance. Original requirement basis: MINIMAL-CONTRACT-PROPOSAL SHA256 eb1d85d52bbef993831c80f4193a859d6fecdf69afba3960d2ef8c6311dbf97e; original implementation plan SHA256 44ac869e716d6c033731d7acb3b9313fc7b71121b6eecaaca271579f3f98a737. CONTRACT.md states concrete implementation refinements (reserved control subtype, negotiated support, private signed lookup), numeric choices and unsupported sources.

Actual components are connected: helper adapter/process sampler and supervised existing-WS publisher; Hub dedicated credential admission plus ingress/cache; private signed read-only route registered on internalMux; verified-subject VS status lookup; actual renderer source gate, response/generation binding and local expiry painting. The tests do not stand in for deployed components or physical Agent measurements.

`logs/DIRECTED-RESULTS.json` records exact argv, rc and elapsed time for the final directed run. All 13 commands rc0. Go main command is compilation only (no functional tests selected), not a full product build.

Directed functional coverage:

- Go validator/cache: exact/duplicate/null/unknown keys, invalid values/units/age/sequence, original receipt, expiry, sample replacement cadence, current identity, generation and alias isolation.
- Actual Hub entry/read route: 4 top-level tests and 11 rejection subcases; fresh sample, no duplicate renewal, expired data, disabled owner, stale client pointer, shared-key, body-selected identity, wrong stable ID, hosted origin, retired/recreated same name; target/direction/nonce/replay/identity/body bounds. Temporary SQLite + memory identity fixtures and HTTP recorders only; no network listener.
- Helper: 19 tests. Mocked statvfs byte calculations, actual zero, unsafe values/error/rename/symlink/close failure, no path export, original sample age, owned PID/mount/cwd binding and unsupported sources, actual publisher singleflight/late discard, actual app loop on a fake WS with 300s cadence and negotiated old-Hub suppression. No new real CLI or filesystem collector is launched.
- VS: 9 tests (5 existing refresh contracts plus 4 new tests) using handler recorders, temporary SQLite/WAL/SHM state and mocked transport/resource readers. Verified subject precedes lookup; signature/body/subject are bound; failure is unknown; body size/parse failure/caller timeout/two-worker bound; paused cost file, parent directory, inode, metadata and content snapshots unchanged.
- Actual renderer gate: 26 assertions for bytes, source/Agent/generation/schema/type/sequence/range/age/TTL, no replay renewal, unknown/expired, alias handling, actual zero and small nonzero.
- Original A6 unknown guard: 8 observations + full inline compile. Original negative cases still reject VS and self-declared scope.
- Actual refresh harness: existing 17 observations plus 3 Agent-disk status/DOM/expiry/identity cases, inline compile. Existing 300s/5s/auth/visibility/invalidation rules exercised.
- A4 regression: integrity22, history7, managed3, overflow4 all rc0; key renderer integrity/ACK/history functions remain parent bytes.
- Canonical build-test wiring 9 tests rc0; the two new Python suites are registered, test inventory updated. Full build/old global suites are not run.

Independent-review-friendly reverse mutations: remove the Hub current-connection guard via a temporary Go overlay => old_connection test rc1 (accepted invalid source). Remove the actual renderer expiry branch in a temporary source copy => real display gate test rc1 (numeric remained fresh instead of expired). Original candidate source is never replaced by these mutations. Raw results in logs/mutation-*.log and mutation-results.json.

PROTECTED-SOURCE.json records 8 whole files and 11 renderer blocks exactly equal to the parent, including console authorization, broker, event ring, private input contract, identity PG store, page store, VERSION and core renderer auth/integrity/stream functions. SOURCE-PINS.json pins the actual changed runtime/tests. MANIFEST.json pins review files excluding itself.

Reproduction from repository root with Go dependencies cached and Python requirements available:

    GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go test ./internal/agentmetrics ./internal/m8 -run '^(TestAgentDisk|TestValidation|TestLifetimeAndBinding|TestRateAndSendAgeBudget)' -count=1 -v
    PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=tools/sessionhelper:view-service python3 -B -m unittest -v tools/sessionhelper/tests/test_agent_disk_metrics.py
    PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=tools/sessionhelper:view-service python3 -B -m unittest -v test_agent_disk_resource
    node tests/a6-agent-disk-real.cjs
    node tests/a6-agent-disk-unknown.cjs
    node view-service/view-refresh-regression.cjs
    node internal/m8/testdata/renderer_integrity_recovery.cjs
    node internal/m8/testdata/renderer_history_priority.cjs
    node internal/m8/testdata/managed_recovery_result.cjs
    node tests/a4-overflow-preserve.cjs
    python3 -B -m unittest -q deploy/test_build_entrypoints.py
    git diff --check

Earlier author fixture failures are retained where a file log exists: default Python lacked pexpect (existing private build venv fixed this); the old unknown-label fixture asserted superseded tooltip copy; refresh fixture still dereferenced the deliberately discarded VS disk. Initial Go fixture used unavailable testing.T.Context and misunderstood RetireAgent's already-retired bool, both corrected; their initial console outputs were not retained as separate immutable file logs. Do not count those as product counterexamples. Early reuse of the prior VS fixture retained its existing local VS resource readers; final fixture replaces those readers with deterministic values (no target observations or new Agent sampler were run).

Remaining acceptance boundaries: independent CR of this exact tree; combined second-batch merge tree and release packaging/build; supported helper deployment and real authenticated sample/status/browser observation; target clock/capability/path/namespace facts; unsupported hosted/external/tmux sources; polling-profile decision above. None is asserted complete by mock logs. No production command, business DB connection, deployment, reboot, or real join/upload was performed.
