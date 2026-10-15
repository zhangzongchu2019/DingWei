# Helper core review repair (author evidence; independent verdict pending)

Source: review/coder/helper-self-update at 0214773ebdf47d5d2f24bd158d025a50998980ea,
tree 5ad7e486b81f4d53228b6ba4b84383c40c4d2682, parent cfb2bdf8.
Design remains frozen at fc3e4b45. Author/runner: coder56solzzc.

All five findings from independent checkpoint 05ab634d were reproduced before
editing (five tests, five behavioral failures, rc1). That exact original probe
and log are retained. The plain-preflight probe reused an intentionally invalid
entry from the wrong-entry case; the permanent regressions separate a valid
entry for R4 from an invalid entry for R5 so failures remain attributable.

- R1: guard releases trial monitoring only for an exact six-field confirmed
  terminal. Rolled-back, cleared, or replaced pending state terminates/reaps the
  process group and exits75. A real long-running candidate is tested, including
  its PID no longer existing; positive confirmation keeps it alive.
- R2: supervisor TERM/INT is tracked separately from candidate crash. Signals
  forward with bounded termination; pending/deadline remain intact. The test
  starts two actual guard/candidate processes with a real TERM between them,
  verifies persisted start2, then a third actual guard selects factory after
  budget exhaustion. This is a process fixture, not a real Docker test.
- R3: ignored local input names are enumerated before immutable export; private
  names reject without echoing bytes. Real Git ignore rules cover env, key,
  inbox, ledger and credential directory cases. This closes fail-on-mixing;
  no pre-fix archived-secret leak was asserted. Existing allowlist and tracked
  content checks remain. This is not a universal secret detector.
- R4: plain preflight only inspects existing paths/proof. It never creates state
  or marker. Only explicit restart-persistence check initializes update state
  and writes the marker; failed reachable restart checks clean the marker and
  cannot produce proof. Missing normal state returns restart_verified=false.
- R5: preflight recognizes the supported tini/bash/source-env/exact-venv entry,
  validates the effective source PYTHONPATH, and refuses unknown launches. It
  enumerates running containers to reject overlapping source mounts before
  source replacement. Controlled Docker metadata plus actual Python state
  probes establish this; no actual Docker acceptance is claimed.

Verification commands:

    PYTHONPATH=tools/sessionhelper <locked-python> -B -m unittest discover -s tools/sessionhelper/tests -p 'test_update*py' -v
    PYTHONPATH=tools/sessionhelper <locked-python> -B -m unittest discover -s tools/sessionhelper/tests -p test_self_update_offer.py -v
    PYTHONPATH=tools/sessionhelper <locked-python> -B -m unittest discover -s tools/sessionhelper/tests -v
    bash scripts/run-build-tests.sh

Targeted core29 and offer15 passed. Full/build exact results are recorded in
CORE-REPAIR-RECEIPT.json and compressed raw logs. Dependencies locked:
websockets15.0.1, pexpect4.9.0; isolated HOME/TMPDIR, ambient SH/DW/DINGWEI removed.
An initial draft targeted invocation had an unmatched-parenthesis syntax error;
it was corrected before source0214773 was committed and the successful rerun
is separate. It is not counted as a behavioral withdrawal.

Six isolated withdrawals restore only the relevant exact-parent product file
and retain new tests. Each returns rc1 at its intended assertion: running child
still alive, pending unexpectedly None, ignored file not rejected, state created,
wrong entry accepted, shared source accepted. No compile/import failures counted.
The temporary overlay runner and summary are retained with logs.

A development package built from source0214773 is local-only. It has no release
approval and was never uploaded. Any final release must pin the approved combined
source commit; do not relabel this checkpoint package.

Still OPEN: actual new Hub+helper authenticated WS integration and real isolated
Linux Docker unless-stopped/read-only-entry/restart/rollback gates. Local Docker
API access remains denied; authorized executor requested. Hub author reports
first implementation ETA19:15Z, complete22:00Z; interface ACK is not delivery.
No full-feature PASS, source upload, deployment, package publication, or real
Agent operation is claimed. Earlier failed probes and earlier evidence remain
unchanged, not overwritten by this receipt.
