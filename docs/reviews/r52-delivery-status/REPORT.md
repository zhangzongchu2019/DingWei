# R52 delivery status and cancellation

## Scope and finding

- Branch: `togpt60lunazzc/r52-delivery-status`; base `425f6452e757d4474c0ac46b18d68197d878936c`.
- Implementation commit tested before the requested supplement: `28b9f0aebe9db57b8486134d8363bd1193f19fdf`.
- Files are limited to Hub `internal/m8`, View `view-service`, the Hub route registration, and OpenSpec/review evidence. No sessionhelper code or helper ledger was changed.

The delete failure was a state-contract mismatch. At the base, Hub published `cancellable=true` for `queued`, `sent`, and `delivered` (`internal/m8/terminal.go`, changed function now at line 945). A helper can already have taken a sent/delivered item; its inbox cancellation only retracts `received`, and rejects `processing` (`tools/sessionhelper/sessionhelper/inbox.py:401-414`, called by `app.py:1256`). The Hub handled `not_cancelable` without publishing an explanatory delivery event, so View left the delete affordance visible. The baseline `renderer-c2-regression.js` passed but did not exercise this helper-taken/refusal boundary. The new browser suite separately checks that a helper-taken item has no delete affordance and that a refusal event explains why cancellation failed; the Hub state test asserts `sent`/`delivered` are not cancellable.

## Changes

- `internal/m8/terminal.go:945`: only `queued` is published cancellable. After a successful helper socket write, Hub publishes the non-cancellable stage. A helper `not_cancelable` ACK publishes `cancel_not_cancelable`; View displays “已交给 Agent，无法撤回”.
- `internal/m8/delivery_ack_v2.go:79` and `internal/m8/terminal.go:595`: track driver-mode `admission_unknown` deliveries and project them as `unconfirmed_retryable` on a later `turn_completed` in the same Hub key/session. This changes the View projection only; the durable delivery and helper ledger remain unchanged.
- `unknownDeliveries` is an in-memory Hub table. Entries that reached `admission_unknown` before a Hub restart are not available for turn-end convergence after restart; they remain eligible for manual retry. Convergence does not distinguish whether the ending turn began before or after the delivery became unknown.
- The supplemental regressions cover an unknown delivery remaining busy with no retry affordance before any same-session turn ends, and both late `injected` and late `processed` confirmations restoring the normal timeline row and removing retry UI.
- `internal/m8/view_retry_v2.go:56`, `view-service/view_service.py:454,791`, and `view-service/renderer-canary-sse.html:1419`: signed, page-authorized retry uses a deterministic new delivery ID keyed by the original delivery, clones its body and attachment metadata, and can create at most one retry. The original row receives “已重发”; the retry control disappears after acceptance.
- `view-service/renderer-canary-sse.html:1145,1738,1998`: replaces the opaque unknown-state wording and adds a read-only list beside the helper unknown count. It lists retained/current event details; if the helper supplies a count but this page has no detail event, it states that explicitly.
- `view-service/renderer-delivery-status-regression.js` adds eight browser checks and is registered in `tools/run-browser-regressions.py`.

## Verification

- Targeted Hub checks, including retry once, next same-session turn convergence, cancellability stages, and both session event rings: `go test ./internal/m8 -run 'Test(ViewRetryV2ReplaysExactEnvelopeOnce|ConvergeAdmissionUnknownOnNextSessionTurnCompletion|DeliveryUpdateCancellableUsesAuthoritativeState|SessionToSessionReliableAdmissionPublishesBothEventRings)' -count=3` — rc 0.
- Full Hub package: `go test ./internal/m8 -count=1` — rc 0, `217.576s`.
- View tests are included in the build gate. `bash scripts/run-build-tests.sh` — rc 0, `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`. The system Python initially lacked the lockfile's `websockets` and `pexpect`; rerunning with an isolated `/tmp` venv installed from `tools/sessionhelper/requirements-runtime.lock` passed.
- New browser suite direct run before supplement — rc 0, 8/8 checks. Supplemental browser run — rc 0, 11/11 checks, including the no-turn and both late-confirmation cases.
- Full browser runner on implementation commit `28b9f0aebe9db57b8486134d8363bd1193f19fdf` — rc 0, all 36 suites rc 0. The complete machine receipt is [BROWSER-RECEIPT.txt](BROWSER-RECEIPT.txt); generated `2026-11-06T17:49:54Z`.
- Four independent mutations each made its guard fail, then were reverted: broaden `sent`/`delivered` cancellation (Go rc 1); create a fresh retry per request (retry-once Go rc 1); disable turn-end convergence (Go rc 1); restore the old unknown wording (browser rc 2, `UNKNOWN_HAS_CLEAR_COPY`).
- Supplemental fifth mutation, also reverted: require `turn_completed.delivery_id` to match the unknown delivery. It failed specifically in `TestAnotherMessagesTurnEndConvergesUnknownDelivery` with “matching terminal event did not converge unknown delivery”; this is the intended “another message's turn ends ⇒ converge” cell.
- `git diff --check` passed. Final browser receipt records `commit_sha=28b9f0aebe9db57b8486134d8363bd1193f19fdf`.

## Conclusion

**Initial implementation passed; requested supplement pending verification.** The read-only list is bounded by the details retained in the current/replayed session event stream; an aggregate helper count without a matching detail event is shown as unavailable rather than represented by invented content.
