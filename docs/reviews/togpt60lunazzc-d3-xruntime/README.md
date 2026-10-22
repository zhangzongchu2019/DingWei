# D3 cross-runtime fixture repair

## Scope

Base: `integration/spec5ff-r35` at `9e73ff1ff5769c2bf6f0098f5a843d90c232a39c` (tree `377a40d9c265f1b1307a451e71c97a7e35e0cb25`).

This change only updates test fixtures for the current pilot-feedback MCP contract. It does not change product or helper code. The affected test is `internal/m8/TestPilotQuestionAcrossRealHTTPHelperIPCMCP`, which exercises the Python fixture `tests/pilot-feedback/local_roundtrip.py` through real MCP stdio, helper IPC, and Go HTTP in its local test environment.

## Cause and repair

The create fixture omitted required `entry_refs`, so current schema validation rejected the call before returning the expected `result`; the test surfaced `KeyError: 'result'`. The withdrawn baseline run is preserved in `raw/base-withdrawal-red.log` with exit status `1` in `raw/base-withdrawal-red.rc`.

The fixture now supplies `entry_refs: ["A1"]`. It also replaces the obsolete `dw_task_question_input` call with the current `dw_spec_clarifications` technical-post flow, and the Go test server routes that endpoint to `HandleClarification`. The fixture checks that the clarification is recorded and the task remains `planned` pending recheck; it does not impersonate an owner answer or execute business work.

## Evidence

- Target cross-runtime test with `DW_DESKTOP_INTEGRATION_PYTHON=/tmp/a6-review-venv/bin/python`: exit `0`; raw output and command environment are in `raw/target-green.log` and `raw/target-green.env`, with `raw/target-green.rc`.
- Standard full repository gate, without the environment override: `TMPDIR=/tmp/d3g go test ./... -count=1 -timeout=30m`, exit `0`; complete output and timestamps are in `raw/full-go.log`, `raw/full-go.started.utc`, `raw/full-go.ended.utc`, and `raw/full-go.rc`. The environment-specific target test remains opt-in, so the standard gate behavior is unchanged.
- `git diff --check`: exit `0`.

The baseline-red and fixed-green runs use the same test and fixture path; the full standard gate is a separate run and did not set `DW_DESKTOP_INTEGRATION_PYTHON`.
