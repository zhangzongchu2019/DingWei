# Tasks

- [x] Implement bounded-backoff retry of transient read-only pre-submit validation failures.
- [x] Persist busy wait before waiting and keep the same envelope/thread at the FIFO head.
- [x] Send ACK v2 busy only after the local durable wait marker is written.
- [x] Keep identity, cwd, direct-input, authentication, and daemon identity mismatches fail-closed.
- [x] Route transient initialize failures on the first Codex delivery through the durable adapter FIFO before validation starts; startup regression covers transport timeout and temporary RPC error using the real helper worker, adapter, SQLite lifecycle, and synthetic RPC.
- [x] Preserve marker-based restart behavior: null submission marker resumes; non-null becomes unknown/manual.
- [ ] Re-run `build.sh --test-only` on the R1/R2 frozen source with both helper profiles; prior green was at pre-rework source `25af4ce1300dfd55ecd9b8869fecb593a003076e`.
- [ ] Re-run `go test -p 1 -count=3 -timeout=45m -v ./internal/m8 ./internal/store` with `HUB_TEST_PG_STRICT=1` on the rework source; prior result is preserved under the pre-rework source and post-N-1b merge anchor.
- [x] OpenSpec strict: 44 passed / 0 failed; raw log `docs/reviews/r61-n1c-presubmit-wait/evidence/openspec-strict-final.log`.
- [ ] Re-run existing G1–G4 mutation set and verify R1 startup mutation is killed by the durable-wait business assertion; prior G1–G4 evidence is at the pre-rework source.
- [x] Verify both Hub receipt-boundary outcomes: the existing pre-`DeliveredAt` ACK test observes busy→queued; coder56's real old-Hub/helper-process trace after `DeliveredAt` keeps `delivered`, then helper FIFO resumes the same delivery with exactly one `turn/start` and no loss. Review evidence: `review/coder56solzzc/r61-n1c-review-20261118` at `7bf230f1dfa6e2a8836ac7bf40a3ab093847f15b`, `docs/reviews/r61-n1c-coder56/evidence/compat-result-trace.json`.
- [x] Publish the report and evidence coordinates. No main push or deployment.
