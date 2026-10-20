# D3 follow-up: successful send / failed completion write

Author completion evidence for leader review and prodops crosscheck. A single
append successor to `4124a00c5e2a526c21f98fe7ab3af37e0826858c` on
`review/coder/pilot-feedback-impl`; no production logic, protocol, helper,
dependency, database schema, seat, host or deployment change.

Frozen input is Git **tree** `c55213054dca5c257b9d50f125ce329b39f12876`, annotated tag
`refs/tags/evidence/coder/feedback-finish-failure-c5521305` published through
`wego-ai/dingwei` Git. Tree-only fetches use normal fetch rather than a shallow
request with no commit. `INPUTS.json` proves exactly three changed files: the
existing m8 regression file, one added withdrawal, and gates.py tree resolution.
All runtime code is unchanged. Read `REPRODUCE.md`, `REQUIREMENT.json` and the
Git-blob `VERIFY.py`; no source/archive exchange outside Git is involved.

## New regression and fault scope

`TestPilotQuestionSendAcceptedFinishFailureNeverBecomesUnsent` runs the actual
worker with real SQLite outbox and real current MemStore authorization. Existing
fixture supplies a synthetic validated provider binding. Sender returns success
and a nonempty fixture provider ID, then a test wrapper invokes the **real**
`SQLite.FinishQuestionNotice` with a canceled child context. The actual database
call returns `context.Canceled` without persisting completion. Only that child
context is canceled: the worker, outbox and subsequent reads remain live.
This is a completion-write failure injection, not a disk-loss/crash experiment
or real Feishu send. No production fault hook is added.

The reset wrapper counts `DeferUnsentQuestionNotice` calls and delegates to the
real SQLite method if invoked, so it cannot hide a wrong reset behind a no-op.
The test observes three actual worker cycles before asserting:

- Sender accepted, Finish called once with sent=true/nonempty provider ID and
  failed: first persisted state **dispatching**, reset calls **0**.
- Next worker cycle: **unknown**, no new provider attempt.
- Third cycle: still **unknown**; total sender calls **1**, Finish calls **1**,
  reset calls **0**. Error identity must be context.Canceled.

The real production assignment `attempted = true` is removed by withdrawal
`sent-finish-failure-attempted-boundary`. It produces designated assertion
`QUESTION_SENT_FINISH_FAILURE_RETRY_BOUNDARY`, rc1: all three rounds become
queued, sender=3, Finish=3, reset=3. Thus the previously surviving withdrawal
now exposes the actual erroneous repeated sends, rather than an unrelated
assertion/compile error. Original worker source is restored after mutation.
Raw proof: `withdrawals/sent-finish-failure-attempted-boundary.log` and
`withdrawals/RESULTS.json`. Full original 45 cases remain covered alongside it.

## Final gates

All new formal runs use immutable Git exports, isolated HOME, cleared
environment, locked local helper Python, offline Go dependencies, embedded-PG
binary cache only and no external DSN. See `ENVIRONMENT.json`.

- Full default-parallel `go test ./... -json -count=1 -timeout=30m`: rc0;
  65 packages; actions {"skip": 13, "pass": 52};
  30 conditional test skips listed individually in `SUMMARY.json`.
  UTC 2026-10-20T22:21:38.278739+00:00 to 2026-10-20T22:34:02.916182+00:00.
- Vet, targeted store/m8/identity race, canonical each rc0;
  canonical `BUILD_TESTS_OK python=54 shell=2`.
- Complete **46** withdrawals: baseline pass, all designated behavioral reds,
  restored Go/helper-module pass. Raw commands, markers, return codes and logs
  are retained in `withdrawals/`.
- Entire helper/runtime locks, browser sources, production worker/store,
  hosted/consolemux/pgtest and Go dependencies match parent (`INPUTS.json`).
  Prior D3 full helper 773 and browser reads are retained as earlier author
  evidence, not claimed newly rerun; only helper-module baseline/restored and
  canonical are newly run. No old full-Hub restart/production acceptance claim.

No failed new candidate run is overwritten. Prior D3 report/failures and
accepted final full-Go readouts remain in the parent history. This candidate
does not merge D1 `09be4a29`, project_context `4b923f93`, D2 `ddad3cff`; leader
integration must preserve all four sets and rerun integrated gates. D1's
independent six-item PASS does not itself authorize deployment. This report
also grants no deployment, upgrade or seat/host action.

Source/design diff-check is clean. Full evidence diff-check flags only the
whitespace-only HTTP response line in five raw withdrawal logs; exact paths
are in DIFF-CHECK.json. Those raw bytes are preserved rather than trimmed.
