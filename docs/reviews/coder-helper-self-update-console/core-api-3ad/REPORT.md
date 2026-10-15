# Console core API alignment — bounded author evidence

Source `6186f2b9aaf926146728fbbebdd3bd504804e493`, tree `0fd4650cad8c369dda69340a78241bf4fd7a4e99`. Normal merge `34cbba6` includes exact core `3adabde30d56e25bcf145d9d8bd8e36a3d14222e` / tree `0c322bc4608670fac3a4a87d90fe9ce4f7d5a130`. Remote core had advanced to 008d2395 at fetch; this receipt does not silently include it. No core file edited by coder. Admin merges were automatic, preserving the separately scoped config reservation and provision retirement.

Console now calls the frozen names `IssueHelperUpdate(ctx, principalID, agentID, releaseSHA)`, `ListHelperUpdateCatalog(ctx)`, and `GetHelperUpdateJob(ctx, principalID, agentID)` for each owned Agent. No transitional aliases, direct durable job enumeration/writes, client URL or client owner. Existing principal/Origin/target-owner/capability controls remain. Test catalog adds the operations-controlled file_service_origin required by the updated core. Production JS is unchanged.

Author local checks on these exact source/test blobs:
- `DW_SPEC_E2E_PYTHON=/data/ai-dev-data/helper-deps-lock-venv/bin/python go test -race ./internal/m8 -run '^TestConsoleHelperUpdate' -count=1 -timeout 3m`: rc0, 10.105s, actual Console HTTP and Agent WS.
- `NODE_PATH=/tmp/coder-a3-playwright/node_modules PW_CHANNEL=chrome node view-service/console-helper-update-regression.js`: rc0, 12 production-JS Chromium checks, HTTP intercepted.
- `bash scripts/run-build-tests.sh`: rc0, BUILD_TESTS_OK python=42 shell=2, isolated HOME/short TMPDIR, locked websockets15.0.1/pexpect4.9.0 venv first on PATH, seat-specific env removed; existing Go caches reused, no cache purge.

- `DW_SPEC_E2E_PYTHON=locked-venv go test ./internal/m8 ./internal/admin ./cmd/workpulse -count=1 -timeout 15m`: rc0, m8 123.711s / admin 8.620s / workpulse 94.419s. This is standard Go, not full-package race.

The prior fa484940 receipt, six sensitive withdrawals, full m8 race timeout and earlier environment failures remain separate exact-object evidence; this append does not turn that race timeout green. Reviewer scoped cc49/325 verdicts do not automatically cover this new merged source. Core health/confirmation review and actual combined Hub/helper and real Linux Docker gates remain separately OPEN. No package upload, deployment, restart, service or real-Agent action.
