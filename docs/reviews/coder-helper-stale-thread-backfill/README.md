# S-HELPER-STALE-THREAD-BACKFILL

Implementation review evidence for the R36 design `9f182adb`, based on
`integration/spec5ff-r36=c78c66c2`. This is a local test-only implementation
candidate; no production helper, Hub, renderer deployment, or seat was changed.

## Behavior

- The helper reads completed items only after a reconnect on the exact bound
  thread. It subscribes before taking a bounded `thread/turns/list` snapshot;
  an unavailable or invalid snapshot fails closed. It never calls `turn/start`
  or injects a user item.
- Complete assistant text is emitted as a structured View event with stable
  thread, turn, and item identity. The renderer replaces fragments for that
  identity with the full item, is idempotent, and ignores buffered late deltas
  for an item already replaced.
- The event is View-only: it does not append to legacy terminal output or alter
  delivery/processed acknowledgement state. The exact capability value
  `SH_VIEW_ITEM_BACKFILL=item-backfill/v1` is required; absent or unknown values
  keep the feature off.
- Acceptance used this seat's isolated Codex app-server and temporary thread,
  not a dataConnector seat. The isolated empty thread initialized and bound
  successfully, but the server returned `-32600` for `thread/turns/list` before
  its first user message (“thread is not materialized yet”). No user message or
  turn was started. Therefore this verifies the real server's no-turn boundary
  and the helper's fail-closed behavior; completed-item payload behavior is
  covered by the deterministic helper RPC fixture and the real browser
  renderer. It does not claim a model-backed completed turn was generated.

## Validation

| Gate | Result |
|---|---|
| Focused helper tests (`test_codex_item_backfill.py`) | 7 passed |
| Full helper discovery | 817 tests, `OK`, rc 0 (430.384s) |
| Canonical build/test script | `BUILD_TESTS_OK python=57 shell=2`, rc 0 |
| New renderer browser regression | 3 assertions passed, 0 failed |
| Existing renderer regression | Passed |
| Six withdrawal mutations | Each returned rc 1 at its named assertion; see `withdrawals/` |
| Python compilation, JS syntax, `git diff --check` | rc 0 |

The helper test environment was a temporary Python 3.12 venv at
`/tmp/tg60-stale-backfill-venv`, installed from the repository's runtime lock
with hash checking and binary wheels only. The full helper suite ran under
`env -i`, a dedicated temporary `HOME`, and a minimal `PATH`; the first
non-isolated attempt using the machine venv (websockets 17.0.1 and missing
certifi) is not counted as a passing gate. Its failure was caused by lock
mismatch and is retained outside this report as a discarded environment
attempt.

## Evidence files

- `withdrawals/`: six preserved mutation outputs and the expected-red summary.
- `raw/helper-full.log`: final 817-test run.
- `raw/helper-focused.log`: focused seven-test run.
- `raw/canonical.log`: canonical Python/shell gate.
- `raw/browser-new-regression.log` and `raw/browser-existing-regression.log`:
  Playwright runs against the renderer.
