# Frozen acceptance criteria — S-CONSOLE-CARD-STATUS-TS3OPUS55

本地复现同类症状，修复该乱序路径。原故障对齐未证：现网事件流没有留存，已提供全部现有材料，不再等待或索取更多原故障材料。本件不声称已定原故障根因。

The approved product change remains exactly the three lines introduced by d67c44216bf7216854f5bc8963f91c7059a85d41: keep a canonical input_posted waiting entry when a same-delivery native event with a turn arrives; existing trusted Hub delivery_update projection owns promotion and status. Legacy path unchanged. This acceptance increment changes only the regression and evidence; product renderer bytes remain identical to d67c4421.

## 1. Out-of-order final state and negative control

The canonical input_posted exists in the waiting area but has not become a timeline input card. A linked assistant_text or state_change/turn_completed with a turn arrives before the canonical injected update. The final authoritative sequence reaches turn_processed/completed. Fixed regression checks the displayed primary footer equals 回合已结束（完成） **first**, before checking waiting/card-index side conditions.

Provenance off/on × two linked native kinds gives four targeted cases. Fixed18-case suite passes. Served-only guard removal fails exactly these four cases, at the final-state assertion, printing:

`turn_processed final card status; actual=null, pending=1`

Each row also explicitly prints card=false, status=null, pending=1 and pendingEvents=4. The fixed counterpart prints card=true and the final footer. No source file changes during this negative control. Logs: raw/acceptance-chrome-fixed-counted.log and raw/acceptance-chrome-unfixed-counted.log. Command: NODE_PATH=/tmp/coder-a3-playwright/node_modules PW_CHANNEL=chrome node view-service/renderer-card-status-regression.js [--remove-guard]. Negative rc2 is expected and retained.

## 2. Unchanged controls

ACCEPTANCE-COMPARISON.json compares the complete after-snapshots, not only test exit codes. Ten required controls are identical with and without the guard:

- Two ordered canonical input/ACK cases (flag off/on).
- Six missing-delivery_id native input/output/state cases (three kinds × two flags), with the unlinked user echo kept independent.
- Two legacy cases (flag off/on), retaining visible legacy input and answer.

All eighteen cases include the two browser-error checks; fixed18PASS/0FAIL and mutant14PASS/4FAIL. Linked raw user_input queue admission also remains unaffected; the existing canonical echo suppression already handles that path.

## 3. Buffer consumption counts

The actual helper_queued delivery_update naturally buffers before the card exists. For every raw-event case, the test records before pending map keys=1 and pending update events=1. With the guard, canonical promotion builds the card, consumes that buffered update, and the test records after keys=0/events=0. The linked assistant/state mutant loses the index and strands keys=1/events=4 (queued, injected, processed, stale queued); final-state failure exposes this before any other assertion. Counts are directly read from the production maps by the browser fixture and preserved per case.

## 4. Full and related gates

The fresh canonical build and relevant m8 page run are in raw/acceptance-build.log and raw/acceptance-m8.jsonl, with process exit JSON files. The standalone Chrome regression is run explicitly; it is not silently counted as a Python unittest in the canonical inventory. Final counts are recorded below and in ACCEPTANCE-RESULT.json. Product/test file hashes are in ACCEPTANCE-SOURCE.json.

The earlier R45 nine-SKIP condition supplement remains in docs/reviews/r45-integration-3fd26ba6/M8-SKIPS.json, already delivered at d67c4421; this acceptance reruns the current feature's gate because the leader requested final acceptance, not the old R45 skip gate.

All work is single-writer/local. Source exchange only via GongFeng; no force push, deployment, restart, staging/shared DB action or live Agent message.

## Final fresh gate readings

scripts/run-build-tests.sh **rc0**; last line BUILD_TESTS_OK helper_profiles=2 python=27 shell=2. Python unittest total **2486**, failures **0** (helper1092×2 +302 across27 other Python files), shell2/2pass. Related m8 top-level **201PASS/5SKIP/0FAIL** (206total); including subtests313PASS/8SKIP/0FAIL. Fresh process exits and raw logs retained. Chrome **18PASS/0FAIL**; remove-guard **14PASS/4FAIL**, all four red at the final status predicate with actual=null and pending=1. Ten ordered/no-ID/legacy after-snapshots compare exactly equal. Product renderer remains byte-identical to d67c4421. No independent-review or production-incident root-cause closure is claimed.
