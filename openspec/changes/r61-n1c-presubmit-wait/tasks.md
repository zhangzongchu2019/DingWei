# Tasks

- [x] Implement bounded-backoff retry of transient read-only pre-submit validation failures.
- [x] Persist busy wait before waiting and keep the same envelope/thread at the FIFO head.
- [x] Send ACK v2 busy only after the local durable wait marker is written.
- [x] Keep identity, cwd, direct-input, authentication, and daemon identity mismatches fail-closed.
- [x] Preserve marker-based restart behavior: null submission marker resumes; non-null becomes unknown/manual.
- [x] Verify frozen source `25af4ce1300dfd55ecd9b8869fecb593a003076e`: `build.sh --test-only` → `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`; helper profiles Claude/Codex each 1281/1281; raw log `docs/reviews/r61-n1c-presubmit-wait/evidence/build-test-only-final-rerun.log`.
- [x] Run `go test -p 1 -count=3 -timeout=45m -v ./internal/m8 ./internal/store` with `HUB_TEST_PG_STRICT=1`: m8 968.209s, store 251.692s, rc=0; PostgreSQL-backed cases report `server_version_num=180003`; no `interrupted (9)` and no failures. Raw log and skip conditions: `docs/reviews/r61-n1c-presubmit-wait/evidence/m8-store-count3-final.log`, `docs/reviews/r61-n1c-presubmit-wait/SKIPS.md`.
- [x] OpenSpec strict: 44 passed / 0 failed; raw log `docs/reviews/r61-n1c-presubmit-wait/evidence/openspec-strict-final.log`.
- [x] G1–G4 isolated mutations all killed by intended business assertions; logs and SHA256 are recorded in `docs/reviews/r61-n1c-presubmit-wait/evidence/mutations/` and `docs/reviews/r61-n1c-presubmit-wait/SHA256SUMS`.
- [x] Old Hub `501e5b8a` + new helper busy ACK: real Hub websocket test consumed the helper-generated `busy` ACK fixture and observed queued/retryable handling; raw result `docs/reviews/r61-n1c-presubmit-wait/evidence/compat/old-hub-busy-ack.log`.
- [x] Publish the report and evidence coordinates. No main push or deployment.
