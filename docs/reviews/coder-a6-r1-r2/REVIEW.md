# A6 bounded 30s profile and cheap identity admission

Follow-up to independent limited CR of fedfd145/tree2436fdfc and its merge with R2=84b12eec. Direct author parent is 51538e0280ca92717c6eedd5ba847543558e734d (fedfd145 plus documentation-only clarification). Existing branch review/coder/a6-real-disk advances; no integration/release ref is modified.

R1: helper sleeps30s; Hub cache replacement minimum30s; existing page300s and Lifetime360s unchanged. Tests reject a second sample at29.999s and accept it at30s; the actual app loop still uses the same authenticated WS and now sleeps30s. Actual renderer source test proves a sample30s old at the status response remains numeric after a further300s (total330 < 360), then expires at its original TTL.

R2: after recognizing the reserved subtype, the handler rejects body>2048 before I/O, then under h.mu checks the current connection and lastMetricsAttempt. A qualifying attempt records its time before GetAgent/GetUser or payload acceptance. Thus invalid eligible attempts also incur the interval; suppressed messages neither hit the store nor renew/shift the cache. Identity/current-pointer validation after that gate is retained.

New TestAgentDiskCheapAdmission wraps the real memory Identity Store with GetAgent/GetUser counters; it calls the actual production handler. Three messages at0/10/29.999s => one GetAgent and one GetUser, not three pairs. At30s a new valid sample invokes the second pair and is accepted. An invalid first payload consumes the same attempt budget; oversize invokes neither lookup. No fabricated ingress result replaces the handler and no target DB is used.

Directed results in raw/RESULTS.json: A6 Go validator/cache/ingress/read-route matrix rc0; helper19 rc0; actual renderer27 rc0; diff check rc0. These exercise the changed boundaries and retained A6 checks; no old full suite/build/browser/target sampling is repeated. The independent parent+R2 full matrix from the review is not presented as author execution.

Two independent source-overlay withdrawals, each on temporary files:

- Remove R2 cheap preflight block; run TestAgentDiskCheapAdmission => rc1 with failed burst/invalid-first/oversize counter assertions. This detects the exact excess-query defect, not merely a parser rejection.
- Restore SampleInterval300s; run TestRateAndSendAgeBudget => rc1 at the full30s accepted-sample assertion.

Current CONTRACT.md now states the selected30/300/360 profile, pre-I/O attempt gate and explicit S2 mixed-version/unsupported-source table. Hosted/tmux/remote/external sources without owned-process proof remain unknown. S1 reject-count obs is left as a nonblocking follow-up, no log flood added.

For reproducibility use the exact argv in raw/RESULTS.json, with dependencies already cached. The mutation overlays only replace the named source file: remove from the Cheap admission comment through just before metricsBinding, or change SampleInterval30 to300. Candidate runtime files themselves are never mutated by these checks. Logs and source pins are included; this is author evidence, not independent CR or deployment/owner acceptance.
