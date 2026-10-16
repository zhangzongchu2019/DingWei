# Final candidate gate readings

Candidate before this evidence-only commit: `0409600fe7dede92f781a3045848a839f4c01f9b`
(`1039179af4e9777b076b94bec7358ed0d983c999`).  All commands ran in the
clean repository root with UTC system time. No deployment, offer, or live
Agent was used.

## Immutable layout pins

- Factory verifier: r15 `b5faf73c8fe4d5c71720daf4eed5b133b8d45dbd`.
- Candidate artifact: r17 `5c2636ed9f04727b2e2b3de8ec4d80d11dd3c6de`.
- Both are packaged and verified by `package_release.py` loaded with
  `git show <pin>:tools/sessionhelper/package_release.py`.
- Command: `DW_HELPER_E2E_PYTHON=/home/dev/a3venv/bin/python go test ./internal/m8 -run 'TestHelperSelfUpdateEndToEnd/new_layout_rejected_keeps_factory_and_projects_reason' -count=1 -v`
- Exit: `0`; elapsed `3.06s`.
- Result: immutable r15 verifier rejects immutable r17 artifact, helper emits
  `package_invalid`, the matching trusted durable job and Console projection
  retain it, and factory remains running with no candidate slot or restart.

## Full helper E2E matrix after setup repair

The default synthetic setup path retains `io` because it uses `io.BytesIO` to
extract its Git archive. Command:

```text
DW_HELPER_E2E_PYTHON=/home/dev/a3venv/bin/python go test ./internal/m8 -run '^TestHelperSelfUpdateEndToEnd$' -count=1 -v
```

Exit: `0` in `11.343s`. All three subcases passed:
`install_restart_confirm` (`5.10s`), `sha_mismatch_keeps_factory` (`3.17s`),
and immutable `new_layout_rejected_keeps_factory_and_projects_reason`
(`3.05s`).

## Canonical

- First attempted command: `./build.sh --test-only`.
- Exit: `1`; this is retained verbatim in
  `canonical-system-python-first-failure.log`. It fails because system Python
  has no `websockets` distribution metadata.
- Passing locked-runtime command:
  `PATH=/home/dev/a3venv/bin:$PATH ./build.sh --test-only`.
- Exit: `0`; terminal line: `BUILD_TESTS_OK python=47 shell=2`.
- Full raw output: `canonical-locked-runtime.log`.

## Requested Go packages

Command: `go test ./internal/m8 -count=1 && go test ./cmd/workpulse -count=1`

Raw terminal output and exact exit:

```text
ok  github.com/zhangzongchu2019/dingwei/internal/m8  112.136s
ok  github.com/zhangzongchu2019/dingwei/cmd/workpulse  131.831s
exit 0
```

## Directed race and Console browser suite

- Command: `go test -race ./internal/m8 -run 'TestHelperUpdateFailureReceipt(PersistsPackageInvalidReason|PreservesLegacyInstallFailed|SanitizesUnknownReason)|TestConsoleHelperUpdateReasonProjectionIsAllowlisted' -count=1`
- Exit: `0`; output: `ok github.com/zhangzongchu2019/dingwei/internal/m8 8.768s`.
- Command: `NODE_PATH=/home/dev/sse-mux-tooling/node_modules PW_CHANNEL=chrome node view-service/console-helper-update-regression.js`.
- Exit: `0`; output: `TOTAL 14 Chromium helper-update UI checks (HTTP intercepted)`.

## Withdrawal proof

Temporary, uncommitted change removed only `package_invalid` from
`helperUpdateReasonKnown`. Command:

```text
go test ./internal/m8 -run 'TestHelperUpdateFailureReceiptPersistsPackageInvalidReason' -count=1
```

It exited `1` in `1.185s` with `durable sanitized failure reason`, because
the trusted receipt was clamped to `unknown`. The exact one-line removal was
immediately restored; the same command exited `0` in `0.189s`. `git status`
was clean after restoration. The temporary change was never committed.
