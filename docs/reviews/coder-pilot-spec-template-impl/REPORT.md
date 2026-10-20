# D1 light spec template: author candidate

Repository `wego-ai/dingwei`, branch `review/coder/pilot-spec-template-impl`. Approved by ruling 85 (`4dbd95ed`), design `28af2a0eabf795476c9ea9328ccc967cf5bea40d`. Base is the previously handed-off D3 candidate `48befea2bfbd8cbe0685f35be5a8c5fbbfad4576`; this does not assert D3 has independently passed. Review goes to the leader and prodops, not togpt60. All readouts below are author evidence; independent review remains pending.

Runtime freeze: `279f75f6993cb734421b1f65cf617b011d348f36`. Complete reproducible source: `279f75f6993cb734421b1f65cf617b011d348f36`. Their equality and the final source scope are recorded in [COMPLETE-SOURCE-EQUIVALENCE.json](COMPLETE-SOURCE-EQUIVALENCE.json). Later candidate commits add evidence only. No deployment, seat changes, live PRD/Feishu request or host operation occurred.

## Behavior and boundaries

Specs workspace links to the three-step light workflow: traceable source and selected scope; six expanded fields and actual responsibility receipts; human confirmation. Missing/unknown/unclaimed fields stay pending. Manual exports are labelled, server-hashed and immutable; the separately configured document connector remains `pending_config`, with no arbitrary URL fetch or borrowed credentials. No semantic model extraction is claimed.

Parent/current revision/SHA, independent Agent pre-review and human finalize remain enforced. Human responsibility acceptance is not actual execution, test PASS or business signoff. In single-person mode the spec pre-review may use one accepted independent reviewing Agent while later tasks retain different per-case reviewers. Actual reviewing Agents cannot be execution Agents. Full risk flags force full mode; even a pending draft cannot erase a prior full-mode downgrade trail without owner risk-clearance records. Full references/human review do not manufacture external security approval or action authorization. The original STOCK 13 IDs and budgets remain NOT_RUN.

Every confirmed template creates an editing spec and deterministic Spec Kit scaffold, not tasks or results. Later normal/R8 revisions and derives retain mandatory enrollment. Final specs are preserved: actual derive revision 1 plus changed append revision 2 produces a successor; old tasks stay bound to old text and are blocked, with a bounded impact hint. D3 adds optional `new_spec_id` to confirm that real owned direct successor; no SQL-invented final revision is used in the regression.

Invitation lists return bounded summaries and missing counts, not repeated full contracts; the 10-large-draft regression proves a small list and complete authorized detail.

New template tasks require R9's explicit group and accepted per-criterion role pair. Legacy status and task-driver entrances cannot bypass this. Old unenrolled task/proposal behavior is preserved, and ambiguous IDs across the two old task tables fail closed. D2 remains the sole task blocker/wake mechanism; D1 adds no automatic CLI execution.

Storage: additive SQLite `0060_pilot_spec_template.sql`, ten tables plus D3 successor-reference column, through existing `schema_migrations`; PG schema, image and dependency lock unchanged. Old r32.1 store startup/migration/read preservation is tested in a compiled store test binary; no full Hub process restart is exercised. Old writes do not enforce new gates: freeze pilot writes before rollback; destructive table cleanup is not a rollback plan. Hub/helper pilot candidate remains 0.2.3.7; 21 MCP tools. The separately approved oaagent 0.2.3.6 artifact is untouched.

## Measured gates

| Gate | Readout | Evidence |
|---|---|---|
| Whole Go, default parallel | rc0; 65 packages, 52 test packages pass, 13 without tests, 30 explicitly listed conditional skips | final-go/go.json, GO-PACKAGES.json |
| vet / race | rc0 / rc0 | final-checks |
| Canonical | rc0, Python 55 + shell 2 | final-checks/canonical.json |
| Helper full suite | 779, rc0, isolated HOME/cleared environment/locked venv | exact-helper; HELPER-INPUT-EQUIVALENCE.json |
| Withdrawals | 35 designated assertion reds, restored Go/helper positives, launcher rc0 | final-all-withdrawals |
| Browsers | 11 suites rc0; new D1 12 checks, existing Specs 220 assertions; remaining D3/D2/R8/R9/hosted/Console/enroll/groups/task-board pass | final-browser |
| Compatibility | new export + actual old r32.1 store startup/migration/read rc0 | final-compatibility |

Whole Go UTC: 2026-10-20T20:21:54.852709+00:00 to 2026-10-20T20:34:05.262308+00:00. The helper full suite is measured at the same exact final source `279f75f6993cb734421b1f65cf617b011d348f36`. The older helper run and its historical input-equivalence proof remain retained and clearly labelled. Real local MCP stdio -> actual helper IPC -> Go HTTP covers list/get/accept/withdraw without creating a spec or starting a task. Actual PG `pg_locks` observes authorization-first/current-revocation schedules. Real R8 callback + human handling, source/consent/confirm rollback, migration failure atomicity, legacy preservation, successor change and task gates have positive/negative tests. Environment and all conditional skips are retained.

## Preserved failures and fixes

- `preliminary-helper`: unfrozen worktree diagnostic, 779 tests, 19 failures + 1 error, rc1. The direct MCP entry served before the new registration, and the generic transport test lacked the new tool arguments. The later frozen helper full suite passes; the original is retained.
- `withdrawals-initial`: the no-Agent-signoff withdrawal stayed green because a broad missing-field test also matched role invitation fields. Tests now require the actual entry validation path. Receipt-version assertions, the unchanged derived-copy negative and the ungrouped legacy positive were also made specific. Final 35-case evidence uses the corrected real paths and designated markers.
- Earlier `release-gates`, `final-release-gates`, `frozen-gates`, `accepted-source-gates`, `candidate-gates`, `final-candidate-gates` canonical rc1 records are retained. The first anchor lacked the transport argument fixture; subsequent old-source runs reached the old `deploy/test_build_entrypoints.py` count assertion (54 instead of 55). The build-test contract correction was first committed in `bedffe170e65627aa5f3e72e60ff9862ff537546` and retained in the final source. Later list-summary changes and their new regression are included in the final exact-tree gates; no old result is relabelled. An absent race receipt in a stopped gate chain means not run, never PASS.
- All earlier exploratory source anchors are predecessors, not final acceptance claims. Final gate source labels and the equivalence proofs govern reuse.

## Reproduce and verify

Fetch this branch through Git. Run the scripts in `tests/pilot-spec-template/` with the exact source anchors and a new local output directory. They use Git exports, offline Go dependencies and private HOME; the helper Python, browser node_modules and embedded PG binary-cache paths are documented in ENVIRONMENT.json. No network installation is added to canonical and the helper runtime lock remains `5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0`.

After fetching the final docs candidate, `python3 docs/reviews/coder-pilot-spec-template-impl/VERIFY.py --commit <exact-candidate>` verifies every listed Git blob, size and SHA256, including raw logs. SOURCE-MANIFEST.json covers every non-docs tracked file; EVIDENCE-MANIFEST.json covers this evidence directory except itself and the final verification receipt. Independent leader review/prodops crosscheck and any rollout approval remain separate.
