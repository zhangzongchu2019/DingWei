# S-HELPER-BUSY-DRAIN-FLAKE

Baseline r20 d2cc9882da4f535f4bb9e8ad69ce7fcd78c3d23a; branch
review/coder/busy-drain-flake. Only product-adjacent edit is the existing test's
wait/cancellation block. No runtime, config, lock, version or release change.

The original test waits only for pending_inbound to become empty, with a1.5s cap.
The real pending_drain_loop sleeps0.2s before each dequeue; configured settle0.1s
is an additional inter-item pause. Its last popleft empties the queue BEFORE
await process_inbound_message (which uses thread offload); busy_acked_from.clear
occurs only AFTER that await returns. Test polling can therefore cancel in the
middle of the last processing operation. asyncio.run also joins executor workers,
so both fake handled bodies can appear while the drain never reaches ack cleanup.
This is an incorrect test completion/cancellation predicate, not evidence of a
product idle/settle defect or1.5s expiry. The fake adapter exposes a Boolean idle
state, not the real CLI inactivity clock.

Unmodified baseline12 subprocess runs:11green/1red; failure elapsed unittest0.525s
at the ORIGINAL busy_acked_from assertion. The unmodified observation wrapper's
12runs happened green; logs retained, not claimed to reproduce failure. A separate
controlled completion probe adds a permitted awaited-event scheduling gap0.2s
AFTER the real process method returns for two but BEFORE the drain resumes. Fixed
test green; restore exact original test bytes and it is red with
cancelled-before-last-completion and the SAME busy-ack assertion. This delay is a
local diagnostic wrapper, not a product change or acceleration of consumer sleeps.

Fix: wait for pending empty, two handler completions and busy-ack empty together,
with asyncio.wait_for timeout10s. Wrong FIFO, duplicates, pending leftovers or ack
leftovers are not tolerated: original final assertions are byte-identical. FIFO
values remain exactly one,two. Unexpected completed/failed drain tasks surface
immediately. Task cancellation/await is always performed in finally after success
or timeout, preserving teardown and preventing task leakage.

Locked Python3.12.3 / original4pin lock; no installs.50 single-case subprocess runs
are recorded individually with outcomes in FIXED-50.json. Full helper root/cwd
logs and final results are separate. An initial root invocation wrongly specified
-t (top-level) on a nonpackage tests directory and failed discovery; retained as
initial-root-discovery-error, not a product failure or successful gate. Corrected
root command uses PYTHONPATH=tools/sessionhelper and discover -s tools/sessionhelper/tests;
helper-cwd uses discover -s tests. Private HOME per process; no real helper state.

No product code or actual Agent/Hub/host/credential operations. No PG/Docker,
release/image build, upgrade/restart/offer/job/deploy. Owner gates/freeze unchanged.


Final readings: fixed single50/50rc0; helper full730 from repo root rc0(385.420s
subprocess/384.550s unittest), helper cwd730rc0(384.596s/383.764s unittest).
Only original test wait/cancel block changed. Private temporary directories are
removed; no Go cache created. Initial discovery-error logs remain classified and
are not counted as helper test executions. Independent review pending.
