# S-R61-N1a rebase verification on R60 release anchor

## Coordinates and scope

- Base: R60 release anchor `501e5b8a185b20f5c931c8ebb89c16bfd633a01e`.
- Replayed design: `550665a1d76f963a4d7f3a098a931e87cfddc4ae` as local commit `fe68dccc5`.
- Replayed N-1a implementation: `bff47bd50a0f540ee223f688730489cca0c8e2eb` as local commit `20849f91a`.
- The original `r61-n1-busy-wait` branch was not rewritten. This v2 branch is independent and descends from `501e5b8a`.
- The replay changes 13 helper/OpenSpec/design-review files. It does not change `internal/m8`, VERSION, main, or deploy state. N-1b capacity rejection/Hub ACK work remains excluded.
- `git diff --check 501e5b8a..HEAD`: clean.

## Gates rerun

1. OpenSpec 1.16.0 strict, with `env -i`: `Change 'r61-n1-busy-wait' is valid`, rc 0.
2. Full `scripts/run-build-tests.sh`, launched as:

   ```sh
   env -i HOME=/home/dev PATH=/tmp/lockvenv/bin:/usr/bin:/bin scripts/run-build-tests.sh
   ```

   Result: rc 0, `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`. Helper inventory: 97 files. Claude profile: 1275/1275, groups `[1266, 9]`, rc 0. Codex profile: 1275/1275, groups `[1266, 9]`, rc 0. The helper runner used `/tmp/lockvenv/bin/python3`; Go was available on `/usr/bin`.
3. Five isolated mutation runs against the v2 tree. Each mutation was reverted before the next; all returned rc 1 at a behavioral assertion (no compile errors or panics):

   | Mutation | Target | Red assertion |
   | --- | --- | --- |
   | Restore elapsed busy-wait cancellation at the configured cap | `BusyAdmissionTest.test_claude_limit_warns_without_cancelling_late_submission` | `BA_LIMIT_STILL_TERMINATED_WAITER` |
   | Skip persistence of the busy-wait marker | same cap test | `BA_WAIT_NOT_DURABLE` |
   | Omit busy-marker rows from restart recovery | `BusyWaitRecoveryTest.test_restart_restores_busy_head_fifo_and_writer_runs_once_after_release` | first FIFO delivery expected `dw-dlv_aaaa…`, actual `dw-dlv_bbbb…` |
   | Skip the pre-writer submission-boundary marker | same restart/FIFO test | boundary timestamp expected, actual `None` |
   | Reverse durable recovery order (`received_at DESC`) | same restart/FIFO test | first FIFO delivery expected `dw-dlv_aaaa…`, actual `dw-dlv_bbbb…` |

The captured full build log and five targeted mutation logs were kept under `/tmp/r61-n1-v2-*` during verification. Mutation edits were made only in a detached temporary worktree and are not part of this branch.

## Result

PASS for the N-1a replay on `501e5b8a`. The full helper/build gate and all five N-1a protection mutations passed their required checks. This report does not include a capacity-overflow behavior change or any N-1b Hub work.
