# Turn attribution fix A

## Change

When Claude emits a `result`, the driver still emits `turn_completed` and marks the current state terminal. It then clears `_active` under `_active_lock` only if `_active` is the same `_TurnState` object captured for that result. This prevents subsequent assistant text from inheriting the completed delivery's identifiers. No helper version change was made.

The default helper test `tools/sessionhelper/tests/test_claude_turn_attribution.py` covers (1) assistant text immediately following the terminal frame has empty delivery and turn IDs, and (2) a subsequent delivery receives its own new delivery and turn IDs.

## Scope limitation

Fix A does not identify frames by their originating turn. If a delayed old assistant frame arrives after a subsequent `deliver()` has installed its `_TurnState`, the current driver may attribute that frame to the new delivery. The test comment records this limitation; handling it requires a separate protocol/frame correlation design.

## Validation

Mutation validation temporarily removed the `_active` clear. The first test then failed at the required empty-ID assertion while the next-delivery case remained green. Full helper build-test output and final commit coordinates are recorded in the task completion receipt.

## Full build-test gate note

`run-build-tests.sh` was executed with `env -i`, the locked Python venv, and a short `TMPDIR`. Both helper profiles passed (1,170/1,170 each, including the new tests). The overall script returned 1 in the final Hub integration gate: `TestDeviceClaimAcrossActualClientAndPostgres/cancel_then_new` emitted `claim integration child failed; secrets not emitted`. The corresponding memory-backed scenario passed. The same isolated PostgreSQL subtest also failed on the unmodified baseline commit `329eb9f9c45d2d785a0429b01eaa0fa5264fe15d`, so this is a pre-existing failure outside fix A's scope. The deployment entrypoint inventory assertion was updated from 93 to 94 to account for the new default helper test.
