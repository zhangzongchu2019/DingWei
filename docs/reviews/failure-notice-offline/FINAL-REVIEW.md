# S-FAILURE-NOTICE-OFFLINE — pre-REWORK candidate evidence

This report describes exact predecessor1dd0f096d936a3696734b9f849d416166e8ac618. Its independent storage REWORK is superseded by REWORK2.md/REWORK2.json; the two policy/body corrections and current source pins are recorded there. Full helper/build history below remains evidence for the unchanged helper/routing bytes, not an independent PASS.

Author: coder56solzzc. Reviewer: opus50ts. Base: `7651602335267ef2f8b9957d795e0cfddbd9c08f`.
Slice one: `97a920aba0dd261c95089e9e96691e63a26e2686`; this follow-up completes routing and compatibility coverage. This is author evidence, not an independent PASS or deployment permission.

## Result and authority

The store transaction commits eligible failed delivery + deterministic sender notice together. Startup/periodic cleanup works without either endpoint reconnecting. All expired nonterminal states are eligible, including capability_pending; terminal processed/dead/cancelled are untouched. Notice expiry and capped retries cannot notify recursively. Four explicit reason codes and a 30-minute terminal-transition backfill cutoff are enforced. Distinct failed deliveries sharing an original message do not collide in the existing unique source index. No migration is added.

The Hub derives trusted notice context from its persisted source type/provenance and the authenticated endpoint, before either Agent or channel AMR parsing. IPC body routing and reply-origin apply the same ownership-bound persisted authority. Client metadata cannot create it; reserved claims are stripped. The Hub-created kind=reply/no_reply/terminal envelope supplies older routing defense in depth. Normal messages are not suppressed by forged claims.

No helper product source changed. A real local helper subprocess using the unchanged base helper receives an ordinary ACK envelope, injects it through its EchoAdapter, processes a lost-ACK retransmission, restarts with the same persistent inbox, and accepts another same-ID retransmission. The real adapter invocation count is one. Tests separately exercise a real loopback Hub/source WebSocket reconnect and same-ID retransmission. There is no production CLI/Hub/account test.

## Reproducible checks

Run from repository root. Python runtime: websockets 15.0.1, pexpect 4.9.0. Clear seat-specific SH_/DW_/DINGWEI_/CODEX_/CLAUDE_/OPENCODE_ variables and use a fresh HOME for helper discovery. Set DW_HELPER_TEST_PYTHON to the same locked runtime and put that venv first on PATH for the build gate.

- `go test -p 1 ./cmd/workpulse/... ./internal/m8/... ./internal/store/... -count=1 -timeout=20m`: rc0, all five package entries pass.
- `go test -race ./internal/store -run '^TestFailureNotice' -count=1 -timeout=5m`: rc0.
- `go test -race ./internal/m8 -run 'Test(FailureNotice|OfflineFailureNotice)' -count=1 -timeout=5m`: rc0; full non-race Go run also covers the final recursion fixture.
- `PYTHONPATH=tools/sessionhelper python -B -m unittest -v tools/sessionhelper/tests/test_offline_failure_notice.py`: rc0, actual helper/inbox compatibility.
- `PYTHONPATH=tools/sessionhelper python -B -m unittest discover -s tools/sessionhelper/tests -v`: final result in FINAL.json.
- `bash scripts/run-build-tests.sh`: rc0, BUILD_TESTS_OK python=42 shell=2.
- `git diff --check`: rc0.

## Withdrawal evidence

All mutations use disposable overlays/copies, never the submitted tree.

1. Split dead commit from notice insertion: synthetic INSERT failure leaves dead instead of rolling back; store regression rc1.
2. Restore parent AMR/reply implementation: actual Agent parser advances and actual channel outbound is queued; trusted-route regressions rc1. Parent kind=reply rollback-control test rc0.
3. Remove only notice retry-exhaustion guard: raw loopback source receives an unwanted recursive notice; rc1.
4. Delete persistent inbox row before accepting a retransmission in a helper subprocess: actual adapter effects become two, expected one; rc1.

The early recursion withdrawal fixture omitted endpoint fields and returned rc0. It was inconclusive; after correcting only fixture identity the same withdrawal returns rc1. Both logs are pinned, not relabeled. First broad Go/helper attempts failed after local ENOSPC; completed own temporary build homes and old rebuildable Go cache were reclaimed, then final checks reran. Prior successful full helper run preceded only the fixture import-path adjustment; final run is separately pinned. Some subprocess tests emit expected close/resource warnings; use final unittest result, not individual diagnostic lines.

## Bounded promise / known gap

Only a known ACK-capable original session source gets the durable notice promise. Unknown/non-ACK sources retain prior best-effort behavior. ACK loss after inbox processed state, including a subsequent helper restart, preserves the same ID and one injection effect. This does not claim atomic external CLI effect across every arbitrary process crash between injection and durable completion, nor exactly-once network transmission.

Historical retry-exhausted rows may contain only arbitrary LastError detail (e.g. cli_unavailable), not the reason code. They cannot safely be inferred as one of four reasons. Backfill covers only explicitly identifiable recent four-reason rows; this limitation was reported to leader and reviewer. New capped retries are atomic and covered. No production data were sampled to infer history.

Full long logs remain at the author's local evidence directory and are hash-pinned in FINAL.json; this does not imply cross-container delivery. Small primary success/withdrawal logs are included in Git. No deployment, restart, production database change, or actual message to a user account was performed.
