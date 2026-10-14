# A3 O3: align spec IPC wait with helper processing

Exact parent review/togpt/a3 = a202b3fc385dff11136c1e5578befa1da7170cb2. All changes are under tools/sessionhelper. Only dw_spec_create and dw_spec_update pass timeout35s; all other shim request defaults stay5s. Helper spec processing has an aggregate asyncio wait_for30s = grant roundtrip10 + Agent HTTP15 + scheduling/cancellation5; shim35 leaves5 for the explicit result. Grant control, fields, authentication, endpoint/body/verbs, response validation, Hub and Console are unchanged.

Necessary companion change: the existing Agent HTTP urlopen/read was synchronous on the IPC event loop. A wait_for on that loop alone cannot enforce the deadline. Only that existing HTTP segment runs via asyncio.to_thread; HTTP timeout15, parsing and status mapping are retained. Grant/WS operations stay on the original loop. The protected grant methods and pre-HTTP handler segment are byte-identical to the exact parent.

On deadline the helper returns a normal framed IPC response with result.status=error, code=spec_write_deadline_exceeded, outcome=unknown and retryable=false. It never claims the grant was not consumed or the HTTP write rolled back; the message requests checking Console rather than automatic retry. No raw exception or capability is returned. The IPC top-level ok indicates delivery of this structured result, not a successful spec write.

Tests use actual local Unix sockets, actual shim tool handlers and actual IPC dispatcher: create and update handlers each take real20s concurrently and both return success; a real31s handler is canceled at the30s budget and the shim receives the explicit error. A separate actual SessionHelper handler test intercepts HTTP and holds its worker: the IPC deadline still replies while the worker is blocked. No real grant, Hub, HTTP, DB, spec mutation or service operation occurs. Socket timing tests take about50s; no time acceleration for the20/30s requirements. The short HTTP-blocking test narrows only its handler cap to0.1s.

7 timeout/spec tests rc0;24 existing related MCP/IPC regressions rc0. Replacing only spec shim35 with5 on a temporary module makes the real20s success test red with TimeoutError. Restoring the exact parent blocking HTTP segment also makes the blocked-HTTP deadline test red. Raw logs included. No unrelated old full suite/build was run.

Residual bound: asyncio cannot preempt kernel I/O inside the HTTP worker. A timed-out worker may finish later and an HTTP write may already have committed. It cannot emit a late success IPC response after cancellation; this candidate explicitly reports unknown outcome and never retries. This is a bounded IPC-result change, not a transaction rollback or hard real-time guarantee.

Author evidence only; merge by A3 owner and independent review by opus50ts are pending.
