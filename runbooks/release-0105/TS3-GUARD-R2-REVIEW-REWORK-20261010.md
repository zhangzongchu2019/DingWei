# TS3 guard R2: address independent review findings

Recorded UTC: 2026-10-10T07:54:35.659004+00:00
Parent reviewed candidate: e9e44d659296e4205f99d3626d404fadb8caecce. Independent R1 findings dd8f1df763325950f92323542be0c064cec0eab7, 5342 bytes, SHA256 dc66b1bad4884bb3bb689a0712aa0332186b007fdff2b60e652a0f5dcd0d2a59 were fetched, read and verified. R1 code PASS, tests REWORK; this new delta is not independently approved yet.

## Disposition

F1 adopted: run_forever no longer resets delay for every clean close. _run_once records monotonic connection time after successful handshake. Only a connected generation lasting at least 60 seconds resets the delay, for either clean return or error. Short lifetimes keep exponential backoff up to configured cap. The replaced error's existing >=10s floor still applies after healthy reset. Producer completion still returns immediately. Synthetic clock tests cover these paths; no real sleep/network or websocket dependency is used.

F2 addressed in tests: sibling finally cleanup awaits 20ms before setting a completion event. The supervisor must not return before that event is set. Removing the final gather is now detected as FAIL, 0 ERROR.

F3 addressed in tests: formerly unbounded supervisor checks use a one-second synthetic deadline that translates TimeoutError to an assertion failure. FIRST_COMPLETED -> ALL_COMPLETED now yields bounded FAIL, 0 ERROR (outer subprocess ceiling 8s), not a hung suite.

F4 narrow implementation: claim_delivery_for_connection shields only the synchronous claim. If its waiter is cancelled before the item is returned for dispatch, it waits for the thread result and releases that exact not-yet-dispatched item before allowing generation cleanup to finish. Normal successful claims remain processing for their normal handler; no-item cancellation performs no release. The delivery worker calls this function at its claim boundary. This is not a blanket reset of processing rows and does not requeue any already-injected work. Existing release remains conditional on processing and scoped by session/delivery ID. A bounded synthetic thread test proves the targeted race and integration with supervisor cleanup. The test uses in-memory state, not an actual inbox database.

F4 limits remain explicit: cancelling an asyncio waiter still cannot terminate a running thread. Cleanup waits for claim/release completion and can therefore be delayed by blocked synchronous storage. Repeated cancellation during recovery, cancellation after claim handoff (including idle checks/injection), actual Hub redelivery interaction, and end-to-end delivery atomicity remain unproven. No automatic retry/reset/cleanup of production inbox state is authorized.

## Validation and review request

All four isolated suites pass: supervisor 9, guide 5, reconnect 5, claim cancellation 3 = 22 tests. All use AST extraction and stdlib/fakes; claim tests create bounded local synthetic threads, never a helper/model process. Four reverse mutations (remove reap await, ALL_COMPLETED, reset short clean closes, remove claim shielding) each give FAIL with zero errors. Original three R1 mutation observations remain historical. Logs and hashes are retained in ts3-guard-evidence-20261010.

R2 review scope is expanded only to run_forever, the connection-time marker, the exact pre-dispatch claim cancellation helper and its delivery-worker call, plus tests F2/F3/new backoff/claim. R1 guide/source boundary PASS is referenced, not reopened. Root authored this delta and requests fresh per-artifact qualification for new areas before independent CR. Product cursor/replay/view semantics remain excluded. No deployment, integration, restart, API/model/PG/SSH or production storage operation occurred. Seven FIELD and incident root cause remain open.
