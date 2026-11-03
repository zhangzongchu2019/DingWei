# R50 R15 session history implementation handoff

Status: implementation handoff for independent pre-review; this is not an acceptance claim.

## Scope and current tip

Branch: `review/togpt60lunazzc/r50-r15-impl`

Tip: `9f969b77e` (`Add R15 view handoff and failure regression`). The implementation and View changes are in commits `804dbcb3` and `9f969b77e`.

The feature switch is `DW_SESSION_HISTORY_PERSIST_ENABLED`; only the exact value `1` enables the store. When off, startup does not open or create `workpulse-history.db`, terminal session events are published in their original r49 form, and the history endpoint returns the stable 503 response. The independent history migration remains isolated from the primary database and version metadata.

## Disabled-mode equivalence criteria

With the switch off:

1. SSE bodies contain neither `_history` nor `_history_gap`.
2. A clean data directory receives no `workpulse-history.db` file.
3. The View does not show the “向上翻页” button after the history endpoint returns 503. The button's `hidden` state is enforced in CSS despite the shared `.text-btn` display rule.

The Go regression tests assert each backend condition. The Chromium regression checks the visible button state. A mutation removing each of the three backend guards failed its corresponding test; removing the CSS hidden rule failed the browser test.

## History request failures and live stream

The View silently ignores a non-2xx history response and always clears `historyBootstrapPending` in `finally`, then drains queued live SSE events. The regression exercised 503, 401, 404, and 500. For 503, the mocked disabled-mode SSE event has no history metadata and the button remains hidden. For 401/404/500, a live event carrying a history cursor is still rendered; a pagination control may be surfaced by that cursor even though the history fetch itself failed. Removing the bootstrap reset was killed by the browser regression.

## View runtime inventory

| Runtime path | Git blob | Purpose |
| --- | --- | --- |
| `view-service/renderer-canary-sse.html` | `61f72d0387d39e3144b92c026932b4aa552caa13` | History bootstrap, pagination UI, error cleanup, and disabled-state visibility |

The browser regression source is `view-service/r15-session-history-browser-regression.js`; it is a test input, not a View runtime artifact.

## Verification

- `go test ./cmd/workpulse -run '^TestSessionHistory' -count=1` — pass.
- `go test ./internal/m8 -run '^TestSessionHistory' -count=1` — pass.
- `go test ./internal/store -run '^TestSessionHistory' -count=1` — pass.
- `NODE_PATH=/tmp/r49-r5-pw/node_modules node view-service/r15-session-history-browser-regression.js` — pass; pagination, anchor stability, 503/401/404/500 live-stream release.
- Five targeted guard mutations were killed by their corresponding tests.
- `git diff --check` — pass.

The broad combined Go package run was interrupted because it entered unrelated long-running integration coverage; the focused R15 suites above completed successfully.
