# Paused cost accounting: interpreter boundary

This clarification supersedes the 5a review package claim that module-level
validation prevents all bytecode writes during a rejected startup. The 5a
commit and its original package remain unchanged.

Python can write `view_service.*.pyc` before executing that module's configuration
validation. The VS guard rejects an unsafe paused configuration and protects
subsequent local dependency imports; it cannot undo interpreter writes that
already happened. A failed import is therefore **not** cost-directory zero-change
evidence. A shell opening/truncating a redirected log before Python starts is
likewise outside the program's guard.

For a paused deployment, choose an interpreter entrypoint with `python -B` or
`PYTHONDONTWRITEBYTECODE=1` **before Python starts**, or keep the Python source and
all configured cache destinations outside the protected cost directory. Do not
set `sys.dont_write_bytecode` inside `view_service.py` and claim it covers the
module's own import cache. Keep page DB and external log destinations separate as
already required. These are deployment preconditions, not a permission to move
site artifacts, change configuration, or activate paused mode.

The added single parameterized test observes four synthetic starts: unsafe
co-location rejects with rc1 but creates the module's cache; `-B` co-location,
environment-disabled co-location, and source-outside-costdir imports succeed
with the cost-directory content/inode/metadata snapshot unchanged. The unsafe
case is an explicit boundary observation, not a waived zero-change PASS.

T2's zero-new-file/sidecar/inode guarantee applies to correctly launched,
validated paused operation. Startup acceptance evidence must include the actual
entrypoint/cache layout and a before/after directory snapshot. Invalid launchers
that perform pre-validation writes must be reported as unsafe; do not extrapolate
that behavior to the main-script entrypoint or a deployment with external source.

No product code, timeout, cost storage schema, active behavior, consumer, or seed
is changed by this test/document child. Returning to active after a paused
period remains blocked on separately approved E2 evidence and explicit exclusion
of paused completions; this clarification adds no resume or backfill capability.
