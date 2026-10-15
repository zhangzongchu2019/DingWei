# Helper update Console HTTP/UI — bounded author WIP

Exact candidate `8f32debe0cdca0b34fe14c10ca1772d82d4d746b`, tree `56500242d2d71bc13fad3d53a2870823567b300b`. Runtime source `cc49c826fa2a59bcc2048c422c30b20dbf896108`, tree `965195358f44b41ca1a8be572c681b2d1c72ee9b`;8f32 adds only the production-page asset test. The peripheral branch began at exact r12, includes retirement16/pin04/evidence, then normally merged exact core `bf4703a9a260939125e300c780eb9492f06c948d` (no core edits). Core remains WIP, not independently accepted wholesale.

## Delivered peripheral behavior

GET/POST `/api/console/helper-updates` only in new platform. Existing N3 principal guard plus handler active/Origin check; own-Agent precheck before principal-bearing core Issue call. Registered catalog SHA selection only, unknown request fields rejected, max50targetIDs/dedup/per-targetresults. Read-only catalog and owner-job APIs with safe projections: never raw URL, health nonce or lock tuple. Linux/owned/selfhost/active/supported online helper gating, persisted offline version is display-only. No catalog/job writes in adapter.

UI production script is embedded and included by the actual new-platform page handler; legacy page omits it. Lazy-open fetch; single/batch, selected-target checks, errors/stale-response guard, manual-required old helpers, explicit pending-vs-confirmed stages. Remote values use textContent. See `docs/operator/helper-update-console-api.md`.

## Author checks on exact runtime/test blobs

- `GOTMPDIR=/tmp TMPDIR=/dev/shm go test -race ./internal/m8 -run '^TestConsoleHelperUpdate' -count=1 -timeout3m`: rc0/9.868s, including production asset binding. Real authenticated Console HTTP and Agent WS carry an actual core offer; rejects missing/wrongOrigin, anonymous and cross-owner targets (including manager), extra URL/owner fields, trailing JSON/oversized batch. Duplicate IDs collapse, safe status reads and manual eligibility checked. This is not package installation/container E2E.
- `go test ./cmd/workpulse -run '^(TestNewPlatformRouteSweep|TestNewPlatform.*Origin|TestConsoleRoutes)' -count=1 -timeout4m` with tempfs fixtures: rc0/0.300s. Route sweep uses production registration.
- `NODE_PATH=<private playwright> PW_CHANNEL=chrome node view-service/console-helper-update-regression.js`:11actual-production-JS Chromium checks rc0; HTTPintercepted, not liveHub.
- Existing Console browser20/20 and enroll20/20 rc0; group-code restore17plusinline syntax rc0. Private Playwright1.56.1/systemChrome. Initial matrix referred to two nonexistent filenames; no pass invented; corrected actual groups-entry fixture is separately recorded below.
- Surgical Go overlays remove Origin guard, own-Agent precheck, unknown-fields rejection or production asset injection:4intendedbehavioralrc1. Browser overlay changes installed text to false online-success: intendedassertionrc1. Scope is peripheral behavior; redundant core ownership guard still exists.

## Failures and open gates

Initial UI fixture lacked UTF8 charset and emitted mojibake; fixture charset corrected (production page already supplies UTF8). Original failed trace retained, not a product claim. Initial API test failed TempDirENOSPC; same source reran with small fixture TMPDIR=/dev/shm and passed. Later Go build attempt again failed ENOSPC at mkdir and is retained; temporary uncommitted busy-job UI refinement was discarded, so exact runtime remains the already-testedcc49blob.

Actual groups-entry harness did NOT pass: its Go payload subprocess failed; rerun explicitly reported `/tmp/go-build... no space left on device`. All attempts retained, none counted green. Subsequent whole Go/canonical gates need space-sufficient executor; leader asked for support (b5e14ec11395), attribution pending. Earlier canonical42+2zero and full admin/workpulse readings cover retirement runtime16/pin04, NOT this final UI candidate. No final whole-tree build-green claim.

Core R1 file-service origin and R2 protected generic-config namespace repairs are coauthor scope and remain separate. Final canonical signatures/RUNTIME-CONTRACT plus newer capability/lifecycle core integration are OPEN. Current calls use actualbf470exports; no speculative fake backend. Actual Hub+helper package/install/restart and real Linux Docker matrices remain OPEN. No full-featurePASS, no source-package upload, deployment or real Agent operation.
