# R46 browser registry and complete runner followup

The card-status suite is now registered as renderer_card_status in tools/run-browser-regressions.py SUITES. Existing environment-input registry selftest passes. The full runner's receipt explicitly includes suite.renderer_card_status.rc=0, so it is no longer a standalone-only probe. Product renderer is byte-identical to d67c4421; this followup changes one registry line plus evidence.

## Full run

Command: NODE_PATH=/tmp/coder-a3-playwright/node_modules PW_CHANNEL=chrome python3 tools/run-browser-regressions.py --receipt docs/reviews/console-card-status-ts3opus55/raw/browser-runner-receipt.txt. SH_/DINGWEI_/DW_/DATAD_ removed; existing test venv on PATH. Initial prerequisite probe found bundled Chromium absent; installed the exact browser revision required by the existing Playwright package (chromium1194/headless), not a dependency upgrade. No browser binary or package uploaded. No source/fixture overrides. Receipt discloses NODE_PATH,PW_CHANNEL.

All26 registered suites executed. Aggregate **rc2**,24 suites rc0 and2 nonzero. New card-status18/18 rc0; requested shared-state suites SSE reconnect26/26, replay sequence13/13, T086 empty-ring2/2 all rc0. Complete per-suite rc and assertion/check counts: BROWSER-SUITES.md / BROWSER-SUITES.json. Count units are explicitly labeled: most scripts count named check groups, console_specs reports220 assertions, the Python suite12 test cases and parity99 output samples. No claim of counting every nested assert() call or mixing those units into a grand assertion count.

Two nonzero suites:

- renderer_0206_file_render rc1:39 checks pass/2 fail. R6 batch/single-file upload wait times out after60000ms; status-bar assertion still requires turnCost, which is absent. Root cause of the upload timeout is not diagnosed here.
- renderer_flood_stress rc2: iframes not found prerequisite assertion. No pressure scenario completes, so no pressure/performance result is claimed.

C2 returns0 with25 hard checks pass and4 existing T-166 known defects. They remain visible in the count table; rc0 is not represented as absence of those defects.

## Failed-suite baseline comparison

After completing the unmodified full26-suite invocation, ran only those two failed suites in detached worktree at exact pre-fix3fd26ba67ebc90db4f35449e6968c4f22f89666b. The baseline's own runner helper was imported and SUITES filtered in the in-memory diagnostic caller to the two names; no baseline files or refs were changed. This is explicitly a two-suite diagnostic, not a full baseline gate. Same Node/Playwright/browser/venv settings, fresh loopback mock and certificate.

Baseline result: file renderer rc1,39pass/2fail with the exact same timeout and turnCost failure signatures; flood rc2 with the identical iframes-not-found assertion. raw/browser-baseline-receipt.txt binds the run to3fd26ba6. This establishes that the observed failures reproduce before this card-status fix in this environment. It does not turn the current full runner into a PASS or grant any release waiver. No product/test expectation was changed to suppress these reds.

## Delivery boundaries

本地复现同类症状，修复该乱序路径；原故障对齐仍未证，不等待更多原故障材料。Previous fresh canonical gate at9f398109 remains rc0/2486Python0fail/2shell pass; it was not rerun for a one-line browser registry addition. This followup completes the requested registry/full-browser measurement and preserves the two pre-existing-at-baseline reds for release-owner disposition. R46 only; no change to staging R45, deployment, restart, shared DB, live Agent messages or external source/file-service transfer.
