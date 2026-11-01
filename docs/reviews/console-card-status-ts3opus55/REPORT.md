# S-CONSOLE-CARD-STATUS-TS3OPUS55

Latest R46 browser registration/full-run followup: BROWSER-RUNNER.md and BROWSER-SUITES.md. Complete26-suite browser gate is rc2 (24rc0,2red); both red suites reproduce with identical signatures at pre-fix3fd26ba6. New card-status and all three requested shared-state suites pass. This is not an all-browser PASS.

Current final acceptance: see ACCEPTANCE.md and ACCEPTANCE-RESULT.json for the leader-approved criteria, fresh full gate,18-case Chrome comparison and explicit buffer counts. 本地复现同类症状，修复该乱序路径；原故障对齐未证，无更多现场材料待补。 Earlier16-case logs below remain historical evidence.

Base: 3fd26ba67ebc90db4f35449e6968c4f22f89666b (r45 /0.2.3.20). Branch: coder56solzzc/console-card-status-ts3opus55. Single writer, GongFeng only. No version change, helper protocol/ACK semantics, backend changes or deployment.

## Reproduction and root cause

The reported symptom is successful delivery with no status on the displayed card; missing delivery_id on a Codex echo was a hypothesis. The original incident event trace has not been supplied, so exact field-incident attribution remains unconfirmed. This report establishes a reproducible defect in the frozen renderer rather than claiming production sampling.

Actual Chrome serving the base renderer, with canonical primary input_posted pending, receives same-delivery assistant_text or state_change with a native turn before Hub delivery_update. admitWaitingDelivery removes the primary from waitingDeliveries and routes the original input_posted into a turn-group. This path never calls renderUser, so it never creates c1InputCards/c1DeliveryCards. rebuildTurn only renders user_input users, so the original primary can disappear from the group as well. Later cli_injected/turn_processed updates enter c1PendingUpdates under the primary message key with no indexed card to receive them. Delivery succeeds, but its displayed card never gets the status. Initial snapshots: raw/initial-probe.jsonl.

Missing ID is not necessary for this failure: replaying the same output without delivery_id leaves the pending primary intact and its final status appears. UnifiedCommandQueue._lifecycle_event emits delivery_id for every queued/admitted user_input. Codex native outputs map turn IDs separately; an unlinked echo must remain unlinked rather than being assigned a delivery by matching text or timing. No delivery IDs, turn IDs or ACKs are fabricated here.

Ordering is not guaranteed by the current helper: app.py registers delivery_ack_outbox_loop and session_event_loop independently in the same connection task group (around lines850–858). The driver event bridge can enqueue native output while lifecycle ACK publication is pending. A renderer cannot use raw output as a substitute for the canonical delivery-update path.

## Fix

In admitWaitingDelivery, retain a pending entry whose original event kind is input_posted. Hub delivery_update remains the only promotion path for that canonical primary; it calls renderUser, establishes its card indexes, then applies the negotiated ACK state. Existing queue/admission echo suppression and monotonic phase handling are unchanged. Legacy user_input promotion remains intact. Independent output and ID-less echoes still render without acquiring another message's delivery status.

Product diff: three added lines (comment plus guard) in view-service/renderer-canary-sse.html. Browser regression: view-service/renderer-card-status-regression.js.

## Regression evidence

The actual-renderer Chrome regression has 16 cases: provenance flag off/on; early assistant_text, state_change, user_input with/without matching delivery ID; legacy input/output fallback; no browser errors. It checks canonical pending survives native events, injected/terminal updates produce one indexed primary retaining its body, same-node terminal updates, stale queued cannot regress terminal, no stranded pending updates, and unlinked echo remains independent. Fixed:16PASS/0FAIL. Removing the guard from the fixed source gives4 failures (linked assistant/state × both flags), rc2; the final mutation modifies only the served page string, with no source change. Earlier on-disk mutation runs restored the source byte-for-byte. Evidence: raw/browser-fixed-native-shape.log, raw/mutation-served-only.log. Raw initial probe confirms pending/card-index loss even without provenance.

Harness development logs are retained: first browser-before.log incorrectly looked for payload in a waiting-row diagnostic label and included the whole metadata-bearing user section in a text assertion; browser-before-corrected.log also observed the existing provenance echo-hold before its timer elapsed. The final regression waits that existing hold for the legacy case and queries .event-content. Mutation uses the final harness and fails only the four targeted cases; these harness corrections did not change product code or status criteria.

Existing browser gates: delivery-ack-v2-browser-regression (46 assertions/0fail), renderer-t074-delivery-id-regression (all pass), renderer-failcard-regression (5/5). Their exact logs are in raw/.

The final harness uses Codex native turn/completed and agentMessage/delta shapes; --remove-guard is a reproducible served-only negative control. Canonical build command uses existing /tmp/r31-build-check-venv/bin on PATH and removes SH_/DINGWEI_/DW_/DATAD_ from the subprocess environment: scripts/run-build-tests.sh. Related Go command: TMPDIR=/tmp/ctclose-go go test -json ./internal/m8 -run 'View|Terminal|Console|Mux|Asset|Invariant|Page' -count=1. Final exit codes/counts are recorded below and in RESULT.json. All fixtures are local; no live Agent message or shared DB write.

## R45 skip-condition supplement

As requested, docs/reviews/r45-integration-3fd26ba6/M8-SKIPS.json supplements the previous review branch's same-named artifact from 2bf3d766c510f7de48faae3d26912edd2c3e4786. Each of its9 original skip records now includes the exact environment variable or prerequisite, source file/line, and predicate. The prior branch is immutable; original observation timestamps and names are preserved. No R45 gate rerun was needed for this documentation change. The current feature's own validation is separate.

No production/root-cause claim from a fixture, real provider call, staging operation, restart, source upload, GitHub push or deployment. Original field trace alignment is still pending; the demonstrated renderer ordering bug and its correction are independently reproducible.

## Final validation result

scripts/run-build-tests.sh rc0, ending BUILD_TESTS_OK helper_profiles=2 python=27 shell=2. Python unittest total **2486**, failures **0** (helper1092 Claude +1092 Codex +302 cases across27 other Python files); both shell scripts pass. Related m8 pages rc0,35.716s: **206 top-level outcomes =201PASS/5SKIP/0FAIL**; including subtests **313PASS/8SKIP/0FAIL**. Optional environment-gated cases are not counted as passed. JSON raw event records preserve their reasons. New browser regression16PASS/0FAIL; served-only guard-removal mutation rc2 with exactly4 targeted failures. Existing ACK46 assertions/T074 all/failure-card5 cases pass. Source manifest and log hashes are retained. This is developer validation, awaiting independent review.
