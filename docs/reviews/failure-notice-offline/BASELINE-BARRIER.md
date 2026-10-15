# R3 fixture completion barrier correction

opus50ts independently ran cb0052a1 current matrix successfully, then observed an old97 fixture rc1: all10 named scenarios passed but final return delta was1. The positive plain return had reached the target wire before replyOriginRouted incremented; closed also increments before replyToOrigin finishes. That independent rc1 remains non-PASS; it is not recast as product failure or silently replaced.

The narrow correction captures positiveStart stats before controls and requires the positive plain return counter to equal positiveStart.replyOriginRouted+1 before taking the notice-phase baseline. The previous closed threshold is retained. This is a semantic completion condition using the production counter written after routing; it adds no fixed sleep, changes no product behavior, and leaves every final notice zero-forward/zero-return/feedback assertion intact. If an unexpected extra positive return occurred, the exact equality would fail rather than absorb it.

On identical corrected fixture bytes, current and exact97 old-routing trees each ran3 repetitions under race:30 named subtests each, rc0, no race. The explicit old97/c7 AMR byte-equivalence proof remains unchanged. Full product/build/helper gates are not rerun for this test-only timing correction. The external CLI remains the clearly marked synthetic local app-server protocol process while helper, driver, event loop, authenticated WS and AMR paths remain real.

The first author old-tree rerun hit explicit mkdir/TempDir ENOSPC before reaching the matrix, rc1. It is included, not passed. Only this task's completed230MiB temporary Go-build directory from the earlier aborted build was removed after confirming no Go processes remained; no source/evidence/production data were deleted. Identical source then passed all3 old-tree repetitions.

Latest fixture pins/logs are BASELINE-BARRIER.json; earlier DRIVER-E2E.json is the exact predecessor fixture snapshot. No integration or deployment.
