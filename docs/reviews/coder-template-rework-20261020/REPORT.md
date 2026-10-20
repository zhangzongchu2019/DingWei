# D1 review successor — six items

Author completion evidence, for leader review and prodops crosscheck. No
self-issued independent PASS, deployment, upgrade, production host or real PRD
operation is claimed. Appends one successor to `bc5d6257cf2d48c82f47ff6894afe6c14113b180`
on `review/coder/pilot-spec-template-impl`; does not rewrite that candidate or
merge the three separate project_context/D2/D3 review successors.

## Frozen source and input exchange

Final input Git **tree**: `b85f7fd6c86f91d9d1a508f948d462b5d7c74a70`; annotated tag
`refs/tags/evidence/coder/template-rework-final-b85f7fd6`.
Earlier diagnostic input tree: `e362f42b8b2c348711328fd6701290e7b9efaf91`; annotated tag
`refs/tags/evidence/coder/template-rework-input-e362f42b`.
Both tags are published in `wego-ai/dingwei`. Fetch independently through Git;
these root trees are not assumed reachable merely from the successor branch.
The final successor is reported by its own exact commit/tree after committing.
`VERIFY.py` reads Git blobs and compares both the final input's entire non-docs
source set and the delivered evidence manifest, bytes/modes/size/SHA included.
Legacy runner receipt `commit` fields contain these tree IDs, not commit IDs.
See `INPUTS.json`, `REPRODUCE.md`, `ENVIRONMENT.json`.

## Required changes

1. `TestPilotTemplateD3ConfirmsActualDerivedSuccessor` adds own/unrelated current
   revision 2, foreign-owner, and archived-successor negatives with exact
   `ErrTaskFlowConflict`, no stored successor link, then the real valid derived
   successor positive. Unrelated uses real create/append. Foreign deliberately
   changes only owner projection of the actual derived successor, isolating the
   owner check; it does not claim a public ownership-transfer route exists.
   Archive uses the actual API. Removing derived/owner/archive guards each hits
   its designated negative assertion.
2. Full contract confirmation and completed R8 review do not make authorization
   permanent. Advancing only the operation clock past authorization expiry
   rejects actual finalize and R9 task start as `ErrConsoleSpecGate`; live-clock
   positives pass. Rejected start writes neither request nor task version.
   Separate finalize/start withdrawals of the revalidation reach their own
   specific operation assertions; proposal/plan clock cases are in a separate
   subtest to avoid masking actual task-start proof.
3. `templateTaskRolesMatch` reads the actual completed, human-confirmed R8 Agent
   for the same spec/revision/request/snapshot inside the SQLite transaction and
   rejects that Agent as developer. This includes human-form executors. Shared
   assignment/candidate-confirmation/execution paths retain this check. Single
   and multi mode tests prove refusal, different owned Agent positive, unknown
   developer refusal, and start refusal for a pre-existing invalid assignment.
   Different Agent IDs under the same human remain allowed. Current PG/R9
   membership/actor permissions remain required.
4. Proposal creation uses caller-stamped `CreatedAt`. Production plan handlers
   pass `h.now()` to new At entry points and audit; R9 start and plan gates pass
   that operation clock through `taskBlockersClearAt`. Legacy public wrappers
   retain real-clock compatibility. Caller-clock create/plan/start withdrawals
   hit specific expiry assertions, without changing process time or expiry data.
5. Visible PM header states 「未评估」不等于「无风险」; full-mode guidance agrees.
   Actual production HTML is exercised by the browser regression.
6. The implemented design now distinguishes the template-confirmed successor
   impact record from legacy derive: deriving an enrolled final spec retains
   enrollment but **does not write an impact record**. No product change or
   claim of old-task impact recording is made for that legacy route.

Detailed mapping: `REQUIREMENTS.json`.

## Validation

All formal final results use immutable final tree `b85f7fd6c86f91d9d1a508f948d462b5d7c74a70` exported to private
local directories, isolated HOME/cleared environment/offline dependencies and
no external DSN; SQLite/embedded PG and loopback browsers only.

- Full default-parallel `go test ./... -json -count=1 -timeout=30m`: rc0,
  65 packages; package actions `{"skip": 13, "pass": 52}`;
  30 conditional test skips (exact names in `SUMMARY.json`).
  UTC `2026-10-20T21:57:41.506956+00:00`–`2026-10-20T22:09:15.284011+00:00`.
- `go vet ./...`, targeted store/m8/identity race, canonical: each rc0.
  Canonical: `BUILD_TESTS_OK python=55 shell=2`.
- 44 real withdrawals: baseline pass → each designated assertion red → restored
  pass, including the pre-existing 35 plus 9 review additions. Raw commands,
  markers and logs in `final-withdrawals/`. Compiler errors do not count as red.
- 11 browser suites rc0. D1 actual HTML: 13 checks, including visible risk copy;
  existing Specs, D3, D2, R8, R9, hosted, Console, enroll, groups and task board
  remain covered (`final-browser/`). These are local fixtures, not real flows.
- Old r32.1 store startup/migrate/read against a new disposable template DB:
  new and old probe rc0. This is **not** an old full Hub restart test, does not
  authorize old writes or operational rollback (`compat/`).
- Entire `tools/sessionhelper` tree, runtime locks and Go dependency files match
  parent exactly (`INPUTS.json`). Original full helper 779 rc0 is explicitly
  reused; only the module baseline/restored helper tests and canonical are
  rerun here. No new full-helper claim or version/dependency change.

## Retained initial results

The earlier tree's `browser/` first run rc1 is preserved: its new assertion
read a closed details element. Final text is now in visible header guidance and
its browser assertion reads that visible element.

Earlier `withdrawals/` stopped with rc1 when the old wrong-pair mutation remained
green: the new pre-review independence guard also rejected the swapped pair.
The role mismatch fixture now uses another owned Agent distinct from the R8
reviewer, so removing only role matching really permits the wrong assignment
and hits its designated marker. Final complete 44-case run is green. Neither
initial result is erased or relabelled. No production guard was relaxed.

Source/design diff-check is clean. Full evidence diff-check flags only line 5
of the two retained preview-current-role-only logs (initial/final), an existing
whitespace-only HTTP response rendered by that withdrawal test. Raw logs retain
those bytes; they are not trimmed to make an evidence whitespace check green.

## Integration / operational limits

This branch still descends from original D3 `48befea2`, retaining D1 schema and
`new_spec_id`. It does not implicitly include the separate review successors
`4b923f93`, `ddad3cff`, `4124a00c`. Leader integration must preserve all four
sets, resolve any overlap in shared task/clock entry points, and rerun final
integrated gates. Predeploy rollback must freeze pilot writes as specified by
the separate D2 runbook; this report grants no deployment or rollback action.
Existing helper support priorities and approved old-layout .6 package remain
unchanged. Independent leader/prodops conclusions are pending.
