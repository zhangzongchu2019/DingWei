# S-BROWSER-BASELINE-FIX review evidence

Delivery is tests/fixtures and evidence only, based on 3fd26ba67ebc90db4f35449e6968c4f22f89666b for r46. No product source, dependency, version, live environment or release branch change. See IMPLEMENTATION.md and VALIDATION-SOURCE.json for exact source coordinates.

## Findings and changes

1. R6 status mock now matches pathname with query; legacy authorization proceeds through the product's real auth refresh. Final full-suite R6_AUTH_PRECONDITION readonly=false; R6_SINGLE_UPLOAD before=27 after=28 delta=1 authorized=true; batch requires six actual upload requests. Negative control only removes query support: raw/mutation-query.log is40 PASS/1 FAIL, sole failure R6_AUTH_PRECONDITION actual readonly=true. This fails the authorization predicate before an upload timeout. Fixed focused file suite41/0 and full-suite41/0. No product bypass.
2. Monetary UI removal authority is historical commit92acc6d02cbcad5f201da9a335381100dff01600, tree da7ca62b9464cd4d0c6a4ee7f327979fc69ce22f, title Remove View status bar cost displays. Exact object text and patch archived in this Git branch; git hash-object verifies the historical commit ID. COST-REMOVAL.json explicitly distinguishes local historical provenance from directly fetchable old history. turnCost/totalCost removed from retained list and asserted absent; no amounts restored.
3. The same status-bar case then revealed a stale raw-VS-disk fixture hidden behind the monetary assertion. Existing base A6 CONTRACT forbids VS filesystem fallback. Test now requires legacy VS values to remain unknown and supplies a bound synthetic Agent-disk sample before checking the unchanged44.2/232.4GB format. Product trust boundary unchanged. Initial intermediate40/1 run retained in raw/focused.log.
4. Flood locator compares origin/pathname and retains query metadata. Additional query-refresh-instance versus pane-dataset equality checks pass. Original pressure, ordering, calm-session and compaction criteria are unchanged and really execute.

## Full browser run

Independent GongFeng ref:coder56solzzc/browser-baseline-fix-validation, ls-remote exact. Validation commit afa8a0050a2f93d1410b91690e5c0ca9e265035d, tree c2f8fd413bedabf89dbb6ef5002280624480aa09. Parents: test implementation3d99c4e8da0f1c873e61f823e4ba62ed1d3969d3 and separately approved card branch a414b53dc19225f2dfedcdf3aaad5f28a6138aec. Base3fd has25 suites; the approved card branch adds26th and its already-reviewed product guard. This independent detached trial merge is the full26 validation scope, not product changes in the delivery branch. Both changed test hashes match the delivery implementation; validation working tree clean after run.

Command: python3 tools/run-browser-regressions.py --receipt <delivery-docs>/raw/full26-receipt.txt. Removed SH_/DINGWEI_/DW_/DATAD_ variables; used existing Playwright and Chrome plus local Python venv. Receipt discloses NODE_PATH/PW_CHANNEL/STRESS_READINGS; no renderer/fixture replacement. Aggregate rc0,26/26 suite rc0, elapsed403.623s. Receipt generated2026-11-01T09:07:45Z. Full raw log retained.

| Suite | rc | Pass count | Hard failures | Counting unit / qualification |
|---|---:|---:|---:|---|
| renderer_0206_file_render | 0 | 41 | 0 | reported cases / named checks |
| console_browser | 0 | 20 | 0 | reported cases / named checks |
| renderer_in_console_iframe | 0 | 3 | 0 | reported cases / named checks |
| renderer_sse_reconnect | 0 | 26 | 0 | reported cases / named checks |
| renderer_failcard | 0 | 5 | 0 | reported cases / named checks |
| renderer_t054_errorboard | 0 | 6 | 0 | reported cases / named checks |
| renderer_t074_delivery_id | 0 | 5 | 0 | reported cases / named checks |
| renderer_card_status | 0 | 18 | 0 | reported cases / named checks |
| renderer_provenance_icon | 0 | 5 | 0 | reported cases / named checks |
| renderer_storage_denied | 0 | 3 | 0 | reported cases / named checks |
| terminal_storage_denied | 0 | 2 | 0 | reported cases / named checks |
| renderer_t086_empty_ring_e2e | 0 | 2 | 0 | reported cases / named checks |
| renderer_replay_seq | 0 | 13 | 0 | reported cases / named checks |
| renderer_upload_ticket_timeout | 0 | 4 | 0 | reported cases / named checks |
| renderer_flood_stress | 0 | 13 | 0 | named pressure criteria |
| renderer_c2 | 0 | 25 | 0 | reported cases / named checks; +4 known T-166 |
| console_specs_browser | 0 | 220 | 0 | explicit assertions |
| console_spec_workflow | 0 | 12 | 0 | reported cases / named checks |
| console_task_flow_r32 | 0 | 11 | 0 | reported cases / named checks |
| pm_spec_flow | 0 | 15 | 0 | reported cases / named checks |
| md_render_core_sync | 0 | 12 | 0 | unittest cases |
| md_render_core_parity | 0 | 99 | 0 | byte-output samples |
| renderer_markdown_image | 0 | 15 | 0 | reported cases / named checks |
| renderer_markdown_injection | 0 | 11 | 0 | reported cases / named checks |
| renderer_failure_dock_embedded | 0 | 4 | 0 | reported cases / named checks |
| renderer_failure_dock | 0 | 3 | 0 | reported cases / named checks |

Counts use the suite's own summary or named PASS checks, not every nested assertion. Do not sum unlike units. C2 still reports four existing permitted T-166 defects, shape-changed0; no new skip/allowlist was introduced. SSE reconnect L4 retains its existing printed-only out-of-window observation, rather than being relabeled as a new assertion.

### Actual pressure readings

| Scenario | Events pushed | Push/s | Events peak/final | Peak DOM | Big sorts | Text identical | Calm complete |
|---|---:|---:|---|---:|---:|---|---|
| flood | 100000 | 5996 | 20001 / 10020 | 55 | 0 | True | True |
| late-order | 30000 | 5988 | 20001 / 10006 | 55 | 1 | True | True |

Both scenarios received their final sequence, did not abort at deadline, and remained responsive. All original13 named pressure criteria pass (flood6 +late-order7). Complete rates, hashes, polling gaps and long-task timings in raw/full26-stress-readings.json; metadata bindings logged as STRESS_FRAME_METADATA.

## Canonical build gate

Run on pure tests-only implementation3d99c4e8da0f1c873e61f823e4ba62ed1d3969d3, command bash scripts/run-build-tests.sh, rc0, elapsed529.921s. Python2486 cases, failures0,31 unittest summaries: helper profiles claude/codex1092 each (90 files each, groups1083+9), plus302 cases across27 other Python files. Both shell gates passed, failures0. Final marker BUILD_TESTS_OK helper_profiles=2 python=27 shell=2. Raw log:raw/build-tests.log; parsed receipts:BUILD-COUNTS.json and raw/build-tests-exit.json. Same stripped environment and existing local Python venv as recorded in VALIDATION-SOURCE.json. Final evidence-only commit does not change these tested sources.

## Delivery and freeze

Only GongFeng wego-ai/dingwei is used for source/evidence exchange. Exact final tip/tree and remote verification reported after the evidence-only commit. Local validation merge is independently identified above. No official integration, deployment or restart performed.
