# S-HELPER-FROZEN-409-MESSAGE

Base: dfc95fd8b6b2a063edd77f22bd38ecc63b729213 (r44). Branch: opus50tc2k/helper-frozen-409. Source rehearsal: review/prodops/r44-staging-freeze-stop @ 77816622c460b00dca2e2122d13137bfe506f7c8, docs/reviews/prodops-r44-staging-freeze-stop-20261101/RESULT.md. That report establishes Hub 409/no-write correctness and the misleading helper revision-conflict message; this change addresses the helper response only.

For create/update HTTP 409, the helper parses the already sanitized Hub detail. Exact error legacy_spec_frozen returns top-level code legacy_spec_frozen, the Hub message (Chinese OpenSpec fallback if missing/invalid), outcome rejected, and retryable false. The existing hub_error remains available. Ordinary revision conflicts retain their original message. Unparseable/unstructured 409 responses receive a conservative message directing the caller to check state without blindly retrying. Other HTTP statuses retain existing behavior.

The existing test_spec_direct.py loopback HTTP fixture now exercises frozen create/update (including missing/empty/non-string message), ordinary revision conflicts containing misleading freeze text, malformed/empty/non-object JSON, redaction, and a matching error code on a non-409 status. No new inventory file or test selection change was needed.

Validation:

- Targeted restored suite: 10 tests PASS; raw/targeted-restored.log.
- Mutation: delete the entire new 409 mapping branch, run the frozen create/update test: rc1, eight failing subcases (missing top-level code). Original source restored in finally, then the targeted suite passes. raw/remove-frozen-branch.log.
- Initial canonical run with system Python: discovery failed due to absent runtime dependencies (including websockets), before execution. Preserved in raw/run-build-tests-initial-missing-deps.log. Switched to existing /tmp/r31-build-check-venv, verified the checked-in runtime lock with check-runtime-deps.py rc0, then ran the unchanged canonical gate. No dependency files were changed.
- Final canonical run: rc0; helper inventory 90 files, claude 1092/1092 (1083+9), codex 1092/1092 (1083+9), both profile rc0; actual SH_DRIVER matched each selected profile. The remaining 27 Python files and two shell gates completed; final marker BUILD_TESTS_OK helper_profiles=2 python=27 shell=2. Full log: raw/run-build-tests.log.
- git diff --check: rc0.

SOURCE.json records source hashes and commands. Scope is local tests; no staging/production service, database, freeze policy, version, deployment or restart changed. Delivery remains GongFeng only during the migration transition; no GitHub push.
