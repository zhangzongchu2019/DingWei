# Console helper-update capability alignment — author candidate

Candidate/source `d23c34bc9bd85481b9cf9869361caa6b54377fb6`, tree `ed7f62694efa439a9983cd7f3afae4fa39618a13`. Runtime source `d7aee3c52e69059408e96a0b5d2318333b637816`, tree `5c78a9999cc17d751394fba8159175da43a3fbb7`; later989d1fa/d23c34b add only tests (GETcapability predicate and full Console workspace browser case). Exact core `b91dbfd473f0c2a1591b1724c22893cc2f858579` was normally merged, with no core source edits. The prior retirement/pin/initial-UI receipts remain historical exact-object evidence.

## Stable interface and scope

Confirmed public APIs: IssueHelperUpdateForActor(ctx,actorID,agentID,releaseSHA), ListHelperUpdateCatalogs and ListHelperUpdateJobsForOwner. Console never passes owner/URL or writes catalog/job. Existing principal auth and Origin guard plus handler defense; target owner checked before core actor revalidation. Current version alone is insufficient: actual credential-bound session capability helper_update_v1 is also required. Core manual-required result maps to the same UI feedback. GET and POST are covered: an uncapable new-version helper has no actionable row/no created job, then real capability announcement enables the real WS offer.

Published helper1e5b88ca already advertises the bit only with Linux, Agent credentials and stable launcher; no helper code changed here. The Console can display only reason data actually supplied by the core read API. Source feedback25069742aa3a notes that b91 receipts persist stage but not receipt reason in job (audit only); full failure-reason semantics await core completion, not an adapter audit-scraping workaround.

## Author validation and exact attribution

| Check | Executed source | Result |
| --- | --- | --- |
| Actual ConsoleHTTP/credentialWS capability API+asset race | d7 runtime/test | rc0,10.461s |
| Targeted ConsoleAPI race including committed GETpredicate |989d1fa (same runtime)|rc0,11.420s|
| `go test ./internal/m8 -count=1 -timeout15m`, locked helper Python |989d1fa (same Go blobs atd23)|rc0,119.278s|
| `go test -race ./internal/m8 ./internal/admin ./cmd/workpulse -count=1 -timeout15m` |exactd7|rc1: m8 total900.083s timeout; admin176.706s0/workpulse218.096s0|
| canonical `bash scripts/run-build-tests.sh` |exactd23|rc0,BUILD_TESTS_OK python42 shell2|
| actual production JS Chromium, full Console close/reopen case included |exactd23|12checks rc0, HTTPintercepted|
| Agent groups-entry production payload+Chromium |exactd7,same product blobs|4checks rc0|

Canonical environment: private locked venv websockets15.0.1/pexpect4.9.0, isolated HOME/short TMPDIR, existing Go caches reused. Browser private Playwright1.56.1/systemChrome via PW_CHANNEL=chrome. Earlier broadConsole20/enroll20/groupcode17 green and routeauthsweep green remain documented in ui-wip; unchanged related product blobs. No zero-SKIP assertion or entire-repository-Go PASS is made.

### The m8 full-race timeout is NOT green

Raw output retained. The command had reached `TestProvisionAckIsAuditedAndNotRoutedBackAsError` (only1s into that case); stack was in SQLite runMigrations/newTestHub. No assertion failure or data-race diagnostic precedes the timeout. The case file hub_test.go is byte-identical to r12, but that alone does NOT prove timeout attribution or parent full-race equivalence. No silent timeout increase or removal; no whole-m8-race PASS. Standard m8 and focused race passed separately as above.

### Sensitive withdrawals on current candidate

Four isolated Go overlays (Origin, owner precheck, forbidden extra package fields, production asset injection) each produce intended behavioralrc1; raw outputs/runner included. Removing only the actual capability-read predicate makes the committed GET assert fail on realHTTP `eligible:true`, rc1. Production-JS overlay changes installed to false-success online text: Chromium rc1 at installed-wait assertion. These are assertion failures, not compile-error substitutes. Initial charset/ENOSPC failures remain in earlier receipts.

## Open shared-feature gates

This is Console/retirement scoped author evidence, not independent PASS. Hub review R1origin/R2reserved config/R3health serializer/R4freshness and terminal preservation remain separately open against reviewed core objects; targeted Go success does not close them. Joint helper/Hub nonce+lock contract proposals31443d4206d7/97d6d4b674d3 await confirmation; no helper serializer patch is silently assumed. Actual Hub+helper download/install/restart/confirm and Linux realDocker gates remain OPEN. No package upload, deployment, service operation or real Agent action.
