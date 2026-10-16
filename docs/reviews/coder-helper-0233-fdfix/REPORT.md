# Helper 0.2.3.3 SQLite and MCP repair

This implements S-HELPER-SQLITE-FD-LEAK and S-MCP-SHIM-EXIT on r15. SQLite ownership now includes explicit close after transaction completion. An IPC failure now returns one sanitized tool execution error, leaving the shim available for a later explicit request without resending the failed request.

Owner authorization is implementation and validation only. Reviewer: opus50ts. No deployment, official artifact build/upload, Console action, private-state cleanup or runtime patching is included. Comparison with the owner-side hotfix remains pending Git delivery.

## Baseline and scope

Repository: git@git.code.tencent.com:wego-ai/dingwei.git

Baseline: r15 b5faf73c8fe4d5c71720daf4eed5b133b8d45dbd / tree 430e29b31fdd4b334f69f6ad21efb8171ff94006.

Branch: review/coder/helper-0233-fdfix.

Implementation commit: 2d9c81a74c4e40d2ed6b8ec8b185f694c59a97dc / tree 94b59717963c72eb7690015e7e975173deac790a.

Analysis: 77d06ee6fbceedd377eea28b5ecc02f043c8451a, independently approved by opus50ts in review 68c833ad2ef15b9995fa59f561c7cebd3dd1f009.

The change has no overlapping path with the r15-to-r16 change set. The r16 MCP forwarder, package builder changes, generic bootstrap material and associated r16 tests are not part of this branch. The leader can integrate this repair into each layout line separately. The dependency lock is unchanged, SHA256 5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0. No SQLite schema or state location changes.

VERSION, internal/version/version.go and helper __init__.py all declare 0.2.3.3. The exact three-source release pin is updated to 0.2.3.3; the released-version floor is now 0.2.3.2, so coherently rolling all three lines back is still rejected.

## SQLite ownership

The new sqlite_utils.connection context manager creates one connection, configures row_factory inside its protected scope, preserves the sqlite3 transaction context and always closes in finally. Commit or rollback occurs before close. Existing lock order, timeouts, URI mode and materialized return values remain intact.

| Original opening point | Repair |
|---|---|
| DeliveryInbox factory | Transaction-scoped factory with sqlite3.Row |
| Legacy inbox read | Same owned context, keeping read-only URI mode |
| Codex ledger factory | Same owned context |
| Claude ledger factory | Same owned context |
| OpenCode ledger factory | Same owned context |
| DesktopAdapter factory | Same owned context with sqlite3.Row; desktop_dispatch consumers inherit closure |
| OpenCodeDBReader factory | Same owned context, keeping read-only URI mode |
| Provision audit | Same owned context |
| Desktop Stop hook | Same owned context, keeping mode=rw and 50ms timeout |

scope.json maps all nine baseline opening sites to nine wrapper calls. The single remaining raw sqlite3.connect is inside sqlite_utils.connection and is paired with its finally close. A direct-file Stop hook import remains supported. No persistent shared connection, disabled thread check, forced GC or FD-limit workaround is introduced.

## MCP tool failure boundary

Transport decoding requires a newline-terminated JSON object and boolean ok. Missing/refused/reset IPC and timeouts become fixed categories; empty, malformed or invalid-shape replies become ipc_invalid_response; ok=false becomes helper_rejected. Upload result shape and send outcome shape are checked before use. Helper error strings, reason fields, paths and arbitrary exception text are not returned.

The shared tool dispatcher returns result.isError=true for transport or unexpected handler failures, with the original request ID, outcome=unknown and retryable=false. Invalid tool arguments remain JSON-RPC errors. KeyboardInterrupt/SystemExit are not swallowed. Each invocation still opens one IPC connection; there is no request replay or automatic retry. The desktop reply attempt journal remains authoritative.

The existing pilot, MCP wiring and actual-Hub rejection fixtures now assert this tool execution error contract. The actual-Hub group-removal scenario still checks unread retention, repeat refusal and no outbound desktop reply, and now checks that the shim stays alive.

## Deterministic regression tests

With cyclic GC disabled and RLIMIT_NOFILE=128, ten workloads each run 200 times across all nine opening sites, including inbox lifecycle, three ledgers, desktop status/list/read/pause/resume/wait, both OpenCode queries, provision audit, legacy read and Stop hook. A further 10000 iterations each perform an empty inbox poll and ledger lookup. Fixture DB FDs are zero at every completed iteration; total FDs remain 4 to 4. Milestones 1, 100, 1000 and 10000 all read zero.

Transaction tests verify committed data from an independent connection, rollback on application/SQL exceptions, early return, and exactly one close on normal exit, body failure, commit/rollback failure and row-factory configuration failure.

The shim process tests cover 18 failure cases: absent socket, refused socket, peer reset, timeout, helper rejection, empty EOF, malformed JSON, truncated frame, invalid UTF-8, JSON array/null/scalar, missing ok, wrong ok type, unsafe peer error/reason, invalid send outcome, vsapi rejection and upload rejection. Every case checks exactly one error response, correct category, no private sentinel/path, process survival, tools/list continuity, no implicit retry and successful explicit recovery on the SAME child. An additional matrix runs all nine tools through helper rejection and recovery. Test-only socket deadlines are shortened to 150ms in the child; production timeouts are unchanged.

Both new test modules are wired into the canonical list. It now contains 47 Python test files and 2 shell gates, with its count contract updated accordingly.

## Withdrawal controls

withdrawal.py copies only the runtime package and relevant regression module into disposable directories. It runs a fixed control, removes exactly the repair under test, and reruns. It verifies that the real checkout's source hashes remain unchanged.

| Repair | Fixed control | Repair withdrawn |
|---|---:|---:|
| SQLite close | Exit 0, 3 tests pass | Exit 1; close-count and GC-disabled FD gates fail, first inbox lifecycle retains 17 fixture FDs |
| Shim exception boundary | Exit 0, 4 tests pass | Exit 1; real stdio failure/survival assertions turn red |

The SQLite mutation removes only the utility's finally close. The shim mutation removes only the typed and generic execution-failure handlers at dispatch, retaining argument validation. This independently demonstrates that each corresponding repair is required. No mutation is applied to the product checkout.

## Validation

All required gates passed. evidence/GATES.json records the invocations, return codes, timestamps and FD readings. The source commit above identifies the implementation bytes independently of this evidence-only final commit.

| Gate | Result |
|---|---|
| Canonical build test list | Exit 0, 47 Python files and 2 shell gates |
| Helper discovery from repository root | Exit 0, 709 tests |
| Helper discovery from tools/sessionhelper cwd | Exit 0, 709 tests |
| Go internal/version and full repository | Exit 0, 51 tested packages; 12 packages without test files |
| Separate repair withdrawal controls | Both fixed controls green; both withdrawals red |

The first helper full run reported one HomeIsolation teardown change in the live seat inbox file during its observation window; that run is not accepted as passing. Both final discovery runs use separate outer synthetic HOME/state directories and remove inherited helper configuration before import, while retaining the existing isolation assertions. No isolation rule was weakened and no live state was reset. Initial and final logs are retained. The first canonical run exposed a stale cross-runtime fixture expectation for JSON-RPC rejection errors; that fixture was updated to require the sanitized tool execution error while preserving the no-resend/state checks, and the complete canonical gate was rerun.

Commands use an existing isolated Python 3.12 environment containing the unchanged pinned runtime dependencies. The required invocations are canonical `./build.sh --test-only`, helper unittest discovery from repository root and `tools/sessionhelper` separately, and `go test -count=1 ./internal/version ./...`. No live credentials, production database or production Hub connection is used. The canonical shell gate provisions its own disposable PostgreSQL and uses synthetic identities.

Raw evidence logs are preserved byte-for-byte, including terminal whitespace. Whitespace checks exclude only raw .log evidence; product code and review prose pass the check.
