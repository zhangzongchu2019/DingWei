# CWD import test: shared-lib Python venv fix

## Scope

- Base: `integration/spec5ff-r38` at `30463142e6b0696763934b3f3d9377194e55922c`.
- Change: `tools/sessionhelper/tests/test_cwd_import.py:25` now creates its fixture with `venv.EnvBuilder(with_pip=False, symlinks=True)`.
- Product code, runtime lock, and `LD_LIBRARY_PATH` were not changed. Runtime lock SHA256 remains `5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0`.

## Shared-library reproduction

The host's `/usr/bin/python3.12` is built without a `libpython` dependency, so a private-layout fixture was made from the installed CPython 3.12 shared library. A small ELF entry point calls `Py_BytesMain`, has `RUNPATH=$ORIGIN/../lib`, and links to a copied `libpython3.12.so.1.0` whose SONAME was renamed to prevent fallback to the system loader path. This makes the venv behavior under test observable without `LD_LIBRARY_PATH`: a copied entry point fails to find its private library, while a symlink resolves to the private source `bin` and its adjacent `lib`.

- Before fix: private-layout run of `test_cwd_import.py` returned rc 1; **18 tests, 24 assertion failures**. Child interpreters returned **127** with `libprivatepy3.so.1.0: cannot open shared object file`.
- After fix: same private shared-library layout with the pinned test venv returned rc 0; **18 tests passed**.
- Normal pinned Python 3.12 test venv after fix: rc 0; **18 tests passed**.
- Two independent withdrawal copies changed only the new `symlinks=True` argument back to `False`. Each selected existing test failed rc 1 because the child interpreter returned 127 with the private shared library missing (`raw/withdraw-cut-1.log.gz`, `raw/withdraw-cut-2.log.gz`).

## Required gates

- Canonical `scripts/run-build-tests.sh`, isolated HOME/TMPDIR and lock-matched venv: rc 0, **`BUILD_TESTS_OK python=63 shell=2`**. The script's final marker is at completion; full output is in `raw/canonical.log.gz`.
- Helper suite, isolated HOME/TMPDIR and the same locked venv: rc 0, **961 tests in 424.251s**.
- Targeted changed test file under the locked venv: 18/18 passed on both normal Python and the private shared-library fixture.
- `git diff --check`: clean.

Pinned environment: Python 3.12.3; websockets 15.0.1, pexpect 4.9.0, ptyprocess 0.7.0, certifi 2026.7.22; lock digest matches the repository lock.

## Why the production hooks do not copy the interpreter

The test called `venv.EnvBuilder(...)` directly. CPython 3.12's `EnvBuilder` constructor defaults `symlinks=False` (`/usr/lib/python3.12/venv/__init__.py:47-49`), but the `python -m venv` CLI sets symlinks to true by default on POSIX and passes that value to `EnvBuilder` (`/usr/lib/python3.12/venv/__init__.py:541-554, 581-587`). Product creation paths use that CLI:

- `tools/sessionhelper/run.sh:237`: `"$python_bin" -m venv "$VENV_DIR"`.
- `deploy/ops/hosted-image/Dockerfile:6` and `deploy/ops/hosted-claude-image/Dockerfile:8`: `python -m venv`.
- `tools/sessionhelper/e2e/Dockerfile.matrix:6`: `python3 -m venv`.

Those paths retain the POSIX symlink default and therefore do not reproduce the fixture's interpreter-copy failure. No product change is needed for this test-only issue.
