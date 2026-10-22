# r37 version pins: 0.2.3.10

## Anchors and scope

- Base: `integration/spec5ff-r37` `f10ff1b1a1a05542a28fca7f12e22eb89aec0567`, tree `a32003c8c8083da7ccec3facef650276cd824a3d`.
- Frozen source: `01f87ac2b457d3e2386eba1c699ae351496c4ece`, tree `240a94fa79c93e5301025fb7b54e0bb0fa23f455`, parent exactly the r37 base.
- Only seven version/test-pin files changed: root `VERSION`, Go version constant and release pin test, helper `__version__`, project-context MCP server-info fixture, legacy MCP package fixture, and the Console helper-upgrade regression fixture.
- No capability minimum, Hub protocol, dependency/runtime lock, or production page source changed. `git diff --check` is clean. The only remaining `0.2.3.9` in non-document code is the intentional prior-release text in the new pin test.

The version is `0.2.3.10` consistently in root, Go, and helper. The pin test is now `TestHelper02310ReleasePins`; it checks those three values and keeps runtime-lock SHA `5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0` unchanged. The legacy package and page-regression fixtures use 0.2.3.10; page production source remains version-free.

## Gates

- `go vet ./...`: rc 0.
- `go test ./internal/version ./cmd/workpulse ./internal/m8 -count=1 -timeout=30m`: rc 0 (`internal/version` 0.002s, `cmd/workpulse` 185.344s, `internal/m8` 146.835s).
- Canonical `scripts/run-build-tests.sh`: rc 0, `BUILD_TESTS_OK python=59 shell=2`, run with isolated HOME/TMPDIR and the private lock-matched venv.
- Helper full suite: rc 0, 849 tests in 416.860s, isolated HOME/TMPDIR, minimum PATH, `python -B -m unittest discover -s tools/sessionhelper/tests`.
- Withdrawal red: in a separate detached worktree at the frozen source, only root VERSION was reverted to 0.2.3.9; `go test ./internal/version -run '^TestHelper02310ReleasePins$' -count=1` failed at the named pin assertion (expected rc 1). The source branch was not changed.
- Additional changed-fixture check: Console helper-update browser regression passed 14/14 with Node 22, existing Playwright install, and Chrome. The first optional invocation selected system Node 18 and exited before tests; the corrected invocation passed. Both raw outcomes are recorded.

Environment: UID 1001; Go 1.22.2; Python 3.12.3; venv `/tmp/tg60-stale-backfill-venv` has websockets 15.0.1, pexpect 4.9.0, ptyprocess 0.7.0, certifi 2026.7.22 and matches the checked runtime lock SHA above. Canonical HOME/TMPDIR were `/tmp/r37hc` and `/tmp/r37tc`; helper HOME/TMPDIR were `/tmp/r37hh` and `/tmp/r37th`.

Raw stdout/stderr/rc files are in `raw/`; the withdrawal is an expected red, all other listed gates are green. No deployment or production action was performed.
