# S-CONSOLE-TAB-CLOSE-WHEN-DISCONNECTED

Base: dfc95fd8b6b2a063edd77f22bd38ecc63b729213. Branch: opus50tc2k/console-tab-close. Scope: local Console close path and mux cleanup; no version, deployment, service restart, staging/production access or GitHub push.

## Reproduction and limits

Actual Chrome loads the production Console HTML and mux broker with loopback HTTP fixtures. At the base commit:

- A thrown mux.remove exception prevents the remaining closeTab statements: the clicked tab, iframe and tabs entry remain. A thrown reset-intent step inside the real broker has the same effect. This confirms the proposed failure mechanism; no browser exception trace from the owner's original incident was supplied, so this does not claim forensic proof of that incident's trigger.
- contentWindow null or a throwing getter alone does not prevent closing: the existing refresh invalidation already catches that failure.
- On disconnect, closing the last tab can leave source=true with seats=[]: the previously scheduled reconnect unconditionally reopens an empty SSE. This is a separately reproduced cleanup defect.
- Non-mux iframe SSE closes normally in the base.

Reproduce with CONSOLE_SOURCE_REF=dfc95fd8b6b2a063edd77f22bd38ecc63b729213 NODE_PATH=/tmp/coder-a3-playwright/node_modules node view-service/console-tab-close-regression.js --baseline. The baseline mode records the broken cases; its successful exit is evidence collection, not a product PASS.

## Change

Each close cleanup step catches and logs its own failure, so admin cleanup, iframe notification, mux removal, iframe navigation and DOM removal do not prevent later steps. DOM nodes and the tab map entry are removed and the last remaining tab is activated. iframe navigation to about:blank also stops its standalone transport.

The broker exposes an idempotent independent release path, called both from remove's finally block and by closeTab after the fallible unsubscribe/reset step. It detaches the seat, marks it stopped, clears its timer and queue credits, and schedules the normal server subscription reconciliation. Closing the last seat immediately clears ACK/reconnect timers and closes the SSE. The reconnect callback clears its own timer reference, and open refuses an empty desired subscription set, including delayed/in-flight control completion. Existing explicit reset-intent behavior is retained for successful closes. The normal bounded server recovery metadata lifecycle is unchanged; the regression checks live subscriptions and transport removal, not deletion of unrelated Console state.

## Verification

The regression serves actual product HTML/broker; only HTTP dependencies and iframe renderers are fixtures. Test-only readout observes the real broker seats, queue credits and transport; it is never included in the product page. Cases: disconnect, public remove throwing, internal reset throwing, contentWindow null, contentWindow getter throwing, and standalone SSE. Each fixed case clicks the actual ×, checks DOM/map/offline-overlay removal, remaining tab activation and retained draft, queue count/bytes release, server unsubscribe, and no last-tab SSE/reconnect after the old ACK deadline.

Initial browser attempts exposed a fixture readiness race: the EventSource object existed before its real HTTP stream was open, so injecting packets could occur before a receiver existed. Preserved timeout logs are not treated as passes. The final fixture waits for actual EventSource OPEN and both bound/ready iframes before injecting the event; no product code or timeout/assertion relaxation was needed for that correction.

- Existing Console browser regression: 20 PASS, 0 FAIL; raw/console-browser-existing.log.
- Related m8 pages: TMPDIR=/tmp/ctclose-go go test ./internal/m8 -run 'Test(ConsolePage|Console.*Tab|Console.*HTML|MuxSource|MuxScoped|ConsoleMux|Mux.*Reset)' -count=1, rc0; raw/m8-pages.log. Includes no hardcoded page version and DOM-ID checks.
- New fixed browser regression: six cases PASS, rc0; raw/browser-final-ready.log. Base reproduction with the final OPEN barrier: raw/baseline-open.log.
- Remove close cleanup protection (--mutate=close-guard): rc1, remove-throws click leaves the tab open. Remove independent release fallback (--mutate=release-fallback): rc1, closed seat/queue remains instead of reaching zero. raw/mutation-close-guard-final.log and raw/mutation-release-fallback-final.log. Mutations alter only served fixture source; product files on disk remain fixed. Previous development logs are retained separately.
- Canonical run-build-tests: rc0; helper inventory 90 files, claude 1087/1087 and codex 1087/1087 (1078+9 each), selected profiles matched actual SH_DRIVER; then 27 Python files and two shell gates completed. Final marker BUILD_TESTS_OK helper_profiles=2 python=27 shell=2. raw/run-build-tests.log. Existing runtime lock check rc0.
- git diff --check rc0. Final source hashes match SOURCE.json; no product changes during these gates.

Playwright is installed at /tmp/coder-a3-playwright/node_modules; Chrome executable /usr/bin/google-chrome. Browser commands use NODE_PATH, and the existing regression additionally uses PW_CHANNEL=chrome. Canonical Python/shell gate uses PATH=/tmp/r31-build-check-venv/bin:$PATH with the existing isolated runtime environment. No test inventory or dependency locks were changed.
