# S-R61-N1B-IMPL implementation report

## Scope and source anchor

- Branch: `togpt60lunazzc/r61-n1b-queue-full`.
- Source commit frozen for implementation gates: `c0ec52db044d92a539a5d7010e597d7338787fc1` (tree `189cdd207194be813cdbc168daad1d944b545e8d`). It includes a test-fixture correction so Hub capability and exact retryable-field mutations reach the terminal transaction and fail on protocol behavior, rather than on an unroutable notice fixture.
- Base: N-1a v2 `638cb324`; merged R60 main `206d41b26dab646c0e50a111eed5a45ccf7c72f6` without conflicts; approved design imported from `6a60a1a625ba36e8240866d641050ed83aaa64ef`.
- No `VERSION` change, main push, build artifact publication, preproduction or production action.

## Behavior implemented

The Hub handshake advertises `X-DW-Queue-Full-ACK: queue-full/v1`. A helper advertises `delivery_ack_queue_full_v1` in `session_capabilities` and enables the terminal response only after the exact response header is received on that connection. An eligible local inbox capacity refusal returns ACK v2 `failed / queue_full / retryable=false`. Inbox I/O failures remain `failed / inbox_unavailable / retryable=true`; busy and unknown submission states do not enter the terminal path.

The Hub accepts the new ACK only from the current connection, when ACK v2, driver mode and the helper capability are present, generation/protocol match, no admission evidence exists, and all three failure fields match exactly. The terminal transition and sender-notice insert run in the same SQLite transaction. Duplicate ACKs preserve the terminal result and deterministic notice identity. Session-origin notices use the existing durable `failure_notice` delivery path and recovery/drain; the fixed queue-full text contains no original body. A Feishu sender uses the existing durable outbound `message` queue in the same transaction with the deterministic notice ID. A missing route causes transaction rollback rather than silently terminalizing without a notice.

Compatibility matrix:

| Hub | Helper | Expected result |
| --- | --- | --- |
| new | new, exact response capability confirmed | Capacity refusal is terminal and a durable notice is queued. |
| new | old 0.2.3.40, capability absent | Legacy busy/retryable behavior; no notice. |
| old, no capability response header | new | Legacy busy/retryable behavior; no terminal ACK. |
| old | old | Legacy behavior. |

The four helper-side compatibility probes all return rc=0. Raw ACKs in `evidence/compat/` show: old helper + old Hub → `busy/queue_full/retryable=true`; old helper + new Hub → the same; new helper + old Hub → the same; new helper + new Hub → `failed/queue_full/retryable=false`. The Hub side of these four direct helper-method probes is represented by presence/absence of the negotiated response condition; no standalone old Hub binary was launched. Separately, the full m8 suite exercises a real local new Hub websocket handshake, checks its actual capability header, and tests new Hub behavior with and without helper capability. Thus the protocol boundary is covered, but the four requested combinations were not each run as separate live Hub/helper processes; this limitation remains for independent review.

## Scope note / design deviation

The approved design text describes a new generic `delivery_failure_notice_outbox` for non-session sources. This implementation does not add that table or dispatcher. It reuses the already durable outbound `message` queue for Feishu senders and the existing `failure_notice` session-delivery queue for session senders. Both inserts share the original terminal transaction and use deterministic IDs; absent routable sender metadata aborts the transaction. This avoids introducing a second notice framework, but leaves any non-session/non-Feishu source without a supported queue-full notice route. No such source is claimed as supported by this implementation. This deviation should be explicitly considered in independent review.

N-6 (`session_unavailable` terminal ACK behavior) is unchanged and not part of this implementation.

## Verification

Final evidence is collected against the frozen source anchor above. Raw command output is kept in `evidence/`; `SKIPS.md` enumerates skipped m8/store cases and the stated conditions. A nonzero or interrupted attempt is retained and labeled separately from passing gates.

| Gate | Result |
| --- | --- |
| `SOURCE_COMMIT=c0ec52db... build.sh --test-only`, env-isolated | PASS rc=0; `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`. Log: `evidence/build-test-only-final.log`. |
| `go test -p 1 -count=3 -timeout=45m -v ./internal/m8 ./internal/store`, PG strict | One allowed known Z-3 flake in `TestSessionNameReconnectKeepsNameAndReleaseReuses` (`hub_test.go:3505`, `interrupted (9)`); store passed; no other failing test. Raw log: `evidence/m8-store-count3-final.log`. |
| Z-3 m8 serial run | PASS: 4/5 full-green runs. Round 5 had only `TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent`, `hub_test.go:3746`, `interrupted (9)`; it is on the exact allowed list. No unlisted failure. Summary and each raw log are in `evidence/z3/`. |
| Full helper build gate, both profiles | PASS as part of build gate (helper_profiles=2, Python=27); Claude and Codex profiles. |
| OpenSpec 1.16.0 `validate --all --strict --no-interactive` | PASS, 43 passed / 0 failed, rc=0. Log: `evidence/openspec-strict-final.log`. |
| Compatibility cells | Four helper-side ACK probes PASS rc=0. These are not four standalone live Hub/helper process combinations; see the limitation above and `evidence/compat/README.md`. Real new-Hub websocket protocol checks are in the m8 log. |
| Mutation checks | 9/9 business-assertion failures. Logs are under `evidence/mutations/`; M05's exploratory compile failure was replaced by a compiling mutation that fails on the missing durable notice assertion. |

## Independent review follow-up: R1 and R2

Coder56's review found two missing boundaries in the frozen source. R1: a queue-full ACK was accepted after a durable `delivered` receipt. The store now rejects queue-full terminalization when a nonterminal row has `delivered_at` or `injected_at`; a replay of an already committed matching dead/queue_full row remains idempotent. R2: generic legacy dead-letter updates could enter the newly expanded queue-full notice allowlist without ACK v2 negotiation. The generic update path now preserves the legacy dead/reason transition but excludes queue_full from notice enqueue; only the negotiated ACK v2 terminal transaction can create that notice.

Added regression coverage: store positive pre-persistence queue_full notice, store rejection after queued receipt, Hub websocket rejection after queued receipt, legacy store terminal without notice, and legacy websocket queue_full terminal without notice. Focused results and mutation evidence are to be appended after full gates complete. This follow-up remains unreviewed until coder56 examines the appended source tip.
