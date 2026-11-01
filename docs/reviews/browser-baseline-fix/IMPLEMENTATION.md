# S-BROWSER-BASELINE-FIX — tests and fixtures only

Base:3fd26ba67ebc90db4f35449e6968c4f22f89666b. Delivery branch:coder56solzzc/browser-baseline-fix. No product source, version, runtime configuration or dependency change.

## Cost-display source prerequisite

Found the exact historical Git commit92acc6d02cbcad5f201da9a335381100dff01600, title `Remove View status bar cost displays`, tree da7ca62b9464cd4d0c6a4ee7f327979fc69ce22f, parent219fd487130b0dea0a8320112dcddd18c534f1d1. It deliberately removes turnCost/totalCost DOM, JS bindings/writes and associated styling. This is the source for updating the retained-ID assertion; COST-PAUSED-STARTUP.md is an interpreter/storage boundary and is not misrepresented as a UI ruling.

The old object exists in a historical local Git repository, not in the current squashed GongFeng history. No claim is made that the old SHA is directly fetchable there. Exact original commit-object text and original git-show patch are archived in this reviewed Git branch as COST-REMOVAL-COMMIT.txt and COST-REMOVAL.patch; COST-REMOVAL.json records origin and hashes. An independent fetch of this branch obtains these exhibits; `git hash-object -t commit COST-REMOVAL-COMMIT.txt` verifies92acc6d0. Historical identity is preserved; no pushed history rewritten and no source uploaded to file service.

## Test/fixture repairs

- File renderer status route uses pathname matching, supporting plain status and its query variants, including the real legacy authorization check. CLI-only --without-status-query restores the former exact-match behavior as a negative control; no new environment override or auth bypass.
- R6 verifies actual six-file request count and actual single-file upload delta=1. It explicitly renews the fixture's authorization before the single-file action and asserts readonly=false. The negative control is red at R6_AUTH_PRECONDITION, actual readonly=true, rather than waiting60000ms for an upload that never started.
- Status-bar removed-ID list includes both amounts and retained list excludes them. No product amount display restored.
- Completing that same previously short-circuited status-bar test exposed another stale fixture: it expected raw VS disk values to become Agent disk numbers. Updated only its fixture to the existing A6 contract (docs/reviews/coder-a6-real-disk/CONTRACT.md in base3fd26ba6). It now requires legacy VS values to remain unknown and a synthetic bound Agent-disk sample to be accepted before retaining the original44.2/232.4GB format assertion. No product trust rule is relaxed; this is a browser-format fixture, not new end-to-end Agent authorization evidence.
- Flood locates exact origin and pathname, preserving all query metadata. It additionally checks each frame's refresh-instance query equals its pane dataset instance. Original full-pressure, text-order, calm-session and compaction assertions remain intact. Full readings are retained, not only iframe readiness.

## Focused validation

raw/fixed-file.log: file-render41PASS/0FAIL, rc0; R6 actual upload delta1, authorization true. raw/mutation-query.log:40PASS/1FAIL, sole red at R6_AUTH_PRECONDITION with readonly=true, suite rc1/runner rc2 as expected. No product file mutated by the control.

raw/focused.log preserves the initial intermediate40/1 run that exposed the old disk fixture, plus successful genuine pressure; it is not relabeled as a final all-pass run. First actual pressure readings: flood100000 pushed at5994/s, peak events20001/final10020, peak DOM55, big sorts0; late-order30000 at5984/s, peak20001/final10006, peak DOM55, big sorts1. Both full text and calm-session content complete; original13 named pressure criteria pass. Full26-suite integrated validation and canonical gate will be recorded separately.

## 26-suite validation scope

Base3fd26ba6 itself has25 registered browser suites. The approved card-status branch a414b53dc19225f2dfedcdf3aaad5f28a6138aec adds the26th suite and its separately reviewed3-line product guard. Keep this delivery branch pure tests; use a separate detached local trial merge with that already-approved branch for the requested full26-suite validation. Do not imply the card-status product change was authored or delivered in this tests-only branch. Exact merge parents/tree, source hashes and receipts will be reported; no official integration/release branch is written, and no deployment is authorized or performed.
