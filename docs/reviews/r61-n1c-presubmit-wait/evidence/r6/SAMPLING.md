# R6 sampling record

Target: `tests.test_driver_failure_faces.HubDeliveryPathTest.test_handle_delivery_turn_auth_failure`.

Each loop ran the single unittest serially 200 times, with `env -i`, a separate HOME/TMP directory, `PYTHONDONTWRITEBYTECODE=1`, and `/tmp/lockvenv/bin/python3`.

| Tree | Result | Failure indices |
| --- | --- | --- |
| Pre-fix N-1c `52a3f8a7041d5fdcf7f3eac6f1e118c1a277adf3` | 198 pass / 2 fail | 12, 159 |
| Baseline `d6e56a8b13bcb622eb0610e4a7cd129f834d9ff3` | 200 pass / 0 fail | — |
| Pre-fix N-1c with RPC method trace | 199 pass / 1 fail | 39 |
| Repaired source | 200 pass / 0 fail | — |

The failed state was empty, while the test expected `failed`. The traced failure's RPC method list was exactly `initialize`; recovery then logged that daemon startup failed and quarantined the FIFO. Thus the intermittent original case stopped before `thread/start` and `turn/start`. The deterministic regression pins that initial pre-transport watchdog interval, verifies `initialize` auth rejection ends in `failed`, and detects removal of the watchdog guard. Raw individual logs for the repaired sample are in `postfix-200-raw.tar.gz`; pre-fix failures and result lists are in the adjacent files.
