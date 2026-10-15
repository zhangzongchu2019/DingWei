# S-GROUP-CROSS-OWNER slice 2: author delivery

UTC 2026-10-15T01:58:43.466428+00:00. Product/test commit 5c2b6e7ee895412d9a46db53633e1ee7ba58e2f3; tree 302f6103390be669fc338b304c1b85b222ac7461. Direct base is reviewed-pending slice 1 2dc9918c7e0671779ba89b89ea4c0eaca3eefad7, not an integration mutation. This is author evidence, not independent CR or release authorization.

## Behavior

- Agent-output @FullName and @member#session now default to expect_reply, same-owner and cross-owner. The existing ! suffix remains compatible. Channels and # legacy selectors retain their old policies. Generic MCP send remains caller-explicit; its tool description explains expect_reply is required for a return. Real MCP tests cover expect_reply success, omitted policy and explicit no_reply not requesting a return.
- On the new platform, explicit member selectors now resolve through the identity user-short-name/full-name directory, rather than the retired legacy member table (a real first-round test exposed that legacy lookup). This path retains the real From and authenticated source key/session. Legacy selector behavior itself remains intact.
- Origin capture and return restore source_key_id/source_session_name when old selector envelopes rewrote From into the recipient namespace. A source-binding test exercises this wrong-namespace envelope, not only modern already-correct From values. The durable delivery must belong to the emitting key/session.
- A private Hub-only return marker lets the automatic reply use its durable exact source address instead of owner-local short-name resolution. Session ingress strips any client claim. The branch requires current source/target Agents and runs the existing live grouping gate before delivery. Removal, deletion and leader-only changes stop return; it does not broaden short/fuzzy name resolution.
- Return envelopes carry kind=reply, reply_policy=no_reply, stable per-delivery/turn dedup identity, and mention_verbatim. The one-hop guard runs before ALL parser branches, including channel mentions. Quoted @Agent/@feishu tokens cannot create new transfers or a ping-pong. Existing reply/hop/dedup tests remain green.
- View-originated mentions remain display-only. Explicit no_reply is honored in the Hub return path; current View mirror tests confirm replies appear only in the correlated View, never the originating helper command path. Normal legacy missing-policy behavior is preserved; current identity-backed Agent sends follow the helper's opt-in policy.

No renderer product, API, migration, deployment or production state change. MCP runtime defaults/IPC timeout values are unchanged; only its tool help text changed.

## Actual validation

All commands run from repository root with GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local TMPDIR=/tmp. The reviewed hash-locked Python venv is /data/ai-dev-data/spec5ff-wave1-validation/venv/bin/python.

- DW_SPEC_E2E_PYTHON=<locked Python> go test -race ./internal/m8 -run '^TestGroupReply' -count=1 -timeout 3m -v: rc0. Real Hub/SQLite/authenticated WS and actual MCP subprocess → Unix IPC → enrolled helper → Hub request → target output → actual source helper inbox. Six top-level suites cover AMR full/member/same-owner A→B→A, removed/deleted/leader-only return denial, caller-explicit MCP policy, restored source and delivery ownership, channel one-hop, and forged-marker stripping. No real model or production system.
- go test -race ./internal/m8 -run '^(TestAMR.*|TestReply.*|TestApplyReplyOrigin.*|TestChannel.*)$' -count=1 -timeout 4m -v: rc0 (127.146s), no data race.
- PATH=<locked venv>/bin:$PATH PYTHONDONTWRITEBYTECODE=1 TMPDIR=/tmp bash scripts/run-build-tests.sh: rc0, BUILD_TESTS_OK python=42 shell=2.
- Nine withdrawals return rc1 at real assertions, not build failures: AMR default policy, source capture, true-source return, exact cross-owner return route, current group gate, channel/Agent one-hop guard, delivery ownership, client marker stripping and MCP opt-in. Logs and WITHDRAWALS.json included. Private overlays are not committed as product source.
- Full streamed -v: DW_SPEC_E2E_PYTHON=<locked Python> DW_HELPER_TEST_PYTHON=<locked Python> go test -json ./cmd/workpulse/... ./internal/m8/... ./internal/identity/... -count=1 -timeout 6m -v: rc1, sole named failure TestOnlineDirectoryBroadcastMarksSingleMirrorPrimary. REPLY-FULL-GO.log is the readable -v stream; REPLY-NONPASS.json includes all opt-in/deferred skips, none counted as a pass.

## Sole full-suite failure is independently reproduced on r5

TestOnlineDirectoryBroadcastMarksSingleMirrorPrimary, hub_test.go:3765: the test dials home/developer/review and broadcasts immediately. Log reports sessions=2/clients=2, then review registration completes; reading review times out. Exact unmodified r5 de7f879b with the same test -count=30 reproduces 3 failures, rc1. Raw BASE-DIRECTORY-REPEAT.log supplied. hub_test.go, terminal.go, BroadcastOnlineDirectories, owner resolver and registration-before-receive block are identical to r5 (REPLY-UNCHANGED-FIXTURE-PROOF.json). The new reply code is outside this directory/broadcast path. Leader assigned this separately to S-M8-FIXTURE-RACES; no test fix has been mixed into this branch. This run is NOT reported as all-green.

## Earlier attempts

REPLY-FIRST/SECOND/THIRD logs retain development findings: new-platform explicit member lookup was still using the legacy directory and was fixed in product; the synthetic Kimi socket needed to be closed before launching another actual helper with the same credential; the test initially searched the list API for bodies and then used the spec-tool nested decoder on the generic desktop result. These latter bridge expectations were corrected only in the new test. The final actual inbox read passes. No product was changed to conceal fixture failures.

Slice 1 remains frozen at 2dc9918c7e0671779ba89b89ea4c0eaca3eefad7; this second branch is separately reviewable. No integration branch changed and no deployment authorized by this report.
