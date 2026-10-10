# TS3 duplicate-send prevention: bounded connection cleanup and guide handling

Recorded UTC: 2026-10-10T07:44:01.066806+00:00

User requirement: prevent the repeated-send/view flooding and helper resource problems described in TS3DEVEG_VIEW_DUPLICATE_SEND_20261009.md and TS3SYNC_VIEW_DUPLICATE_SEND_20261009.md in the new 0.2.10* release series. Implementation base: 8823610f0668397692674e4e82486b3e5c48eed5 (VERSION 0.2.1.105); isolated branch opus50ts/ts3-duplicate-send-guard-20261010. This branch does not change any installed candidate or its earlier source pins.

## Findings and implementation

The two reports concern .95 and record repeated-send symptoms, high helper CPU and recovery after helper-only restart. Neither identifies a confirmed triggering code path; syscall counts are not counts of distinct business messages. Their historical runtime observations cannot establish the current state.

Static inspection of .105 identified two additional reproducible gaps. First, supervise_connection_loops cleaned up siblings on exceptions but waited indefinitely when one connection loop returned normally and other loops kept running. It now waits for FIRST_COMPLETED, propagates errors and cancellation, and cancels/awaits all sibling coroutine tasks before the connection generation exits. The empty-input case remains supported. Existing reconnect backoff remains in force. Cancellation of an asyncio.to_thread waiter does not terminate an already-running worker thread; this patch only proves coroutine cleanup, not thread shutdown or in-flight delivery transaction atomicity.

Second, recv_loop logged every ignored client onboarding guide. It now logs once per connection and yields to other event-loop tasks for each ignored guide. These control frames remain non-injecting and non-sending; normal business and roster behavior remains covered. This bounds diagnostic spam and prevents buffered guide bursts from monopolizing the event loop; it does not rate-limit an upstream sender or promise zero CPU use under arbitrary traffic.

Existing .105 event-bridge subscription identity guards and reconnect backoff are retained. Cursor-based deduplication is deliberately not introduced: distinct deltas can share a cursor. Product replay semantics, actual view duplication and incident root cause remain outside this local test proof.

## Validation

Tests extract only the two target app functions and pure classifiers via AST, with stdlib and in-memory fake WebSocket/helper state. They do not import product startup, construct a real helper, access credentials/home/inbox, or dispatch model/API/SSH/PG calls.

After correcting a missing re binding in the test fixture, the unchanged base fails six assertions across the two suites (three supervisor and three guide tests), with zero test errors. The earlier fixture errors are retained privately as harness history and are not claimed as product defects.

Patched source: 13 tests PASS (8 supervisor, 5 guide), including 20 successive clean disconnects with no old coroutine workers, original exception/cancellation propagation, empty-input compatibility, 200 ignored guides with no CLI injection or send, one log per connection, scheduling fairness, roster handling and business delivery. Three isolated reverse mutations (restore gather, log each guide, remove yield) are each detected as test failures with zero errors. git diff --check passes. Public baseline/fixed/mutation logs are in ts3-guard-evidence-20261010/.

## Release disposition

Actual source and local regressions are ready for independent qualified review. Root authored this patch and cannot sign its own independent review. No deployment, restart, live validation or new runtime GO occurred. All earlier H1/A3/H2/H3 windows remain consumed. Before release integration, obtain per-artifact review qualification and accepted findings, then validate an explicitly authorized candidate under bounded reconnect/guide stress and real view/event observation. Release acceptance must include no duplicate business delivery/view output, bounded CPU/send activity after disconnect, preserved normal messages, and durable evidence; passing these isolated tests alone does not close those field conditions.
