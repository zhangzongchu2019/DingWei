# Antigravity ACP driver follow-up

Base: `14494d4a` on `feat/antigravity-driver-20261120`. This change retains the attachment, read policy, and MCP commits after `4aeca81d`.

## Repairs

- R1: ACP error and cancellation stop reasons now produce terminal receipts that the helper recognizes. Cancellation settles as `interrupted`; other unsuccessful turns settle as `failed`. The receipt retains the turn ID for admission and lifecycle settlement.
- R2: `abort_transport()` closes the stalled ACP transport and wakes pending RPC calls, so the adapter's admission timeout recovery can replace it without waiting for the prompt RPC's one-hour timeout. The unknown delivery is not resubmitted.
- R3: ACP RPC errors, unexpected stop reasons, and prompt exceptions no longer copy peer-provided text into receipt details or operational logs.

## Verification

`PYTHONPATH=tools/sessionhelper python3 -m unittest tools.sessionhelper.tests.test_antigravity_driver -v`: 13 passed.

`PYTHONPATH=tools/sessionhelper python3 -m unittest tools.sessionhelper.tests.test_inbox_lifecycle -v`: 6 passed.

The tests use a local ACP process fixture. Real Antigravity CLI, live DingWei delivery, and independent reviewer sign-off remain open.
