# Design: preserve Agent provenance across admission

## Current behavior and root path

The Hub already derives trusted Agent provenance for authenticated `agent_session` ingress. `internal/m8/hub.go` removes any client-supplied provenance and sets the ingress channel from the authenticated session. `internal/m8/terminal.go:855-883` attaches `provenanceForEnvelope` output to `input_posted`; `terminal.go:896-922` returns a trusted Agent provenance block when the ingress marker is `agent_session` and `env.From` parses to a non-empty session name.

In the renderer, `normalizeEvent` at `view-service/renderer-canary-sse.html:2223-2271` accepts the icon only when the provenance decision is trusted and the block is Hub-attested. `provenanceIconHtml` at `2366-2384` also requires the provenance icon flag and normalized trusted provenance. `renderWaitingDelivery` at `2793-2853` uses the canonical event, so a trusted icon appears while it waits.

For the reported sequence, the `cli_injected` update path at `2602-2622` removes the waiting row and calls `renderEvent(wEntry.event)`, retaining the canonical event and icon. The admitted echo then reaches the display-v1 merge at `2719-2745`: it matches the canonical card by delivery, removes that provisional card, and buffers the admitted echo. When the echo is flushed into turn rendering, `renderTurnEvent` renders the helper event's normalized provenance. Since that event has no provenance block, its icon is absent. The replay/render path at `renderEvent` `3740-3762` has the same identity-loss risk when it moves the canonical card into a turn.

The four-artifact replay located the first 1-to-0 transition at commit `b425568ef3e1d4690de8b37327c28835b7eaaa3d` between `2e1e4c9d` and `c22bb63f`; the earlier candidate `3184933e` was not the introducing change. That commit changed admitted-echo history grouping so the canonical input card is removed and the provenance-less echo supplies the turn input.

The separate `renderWaitingDelivery` replacement at `2818-2819` can replace a waiting entry with an event carrying text, but it is not the cause in this reproduced injected-then-admitted sequence: the injected acknowledgement has already moved the canonical event out of the waiting area.

## Proposed renderer-only behavior

At the existing same-delivery merge boundary, retain the canonical event's already-normalized provenance alongside the admitted echo. Do not copy arbitrary raw fields from the helper event into the canonical event. Keep the admitted event's body, turn identifier, timestamp, and Hub SSE identity/order as the content and placement source. The merge should carry only the canonical trusted provenance (or retain a reference to the canonical event and use its normalized provenance when rendering the merged turn input).

The implementation stores a snapshot only when the visible entry is a `delivery.role=primary` canonical input and its normalized provenance is already trusted. Both live dispatch and history/replay merge call the same retention helper after the existing exact delivery lookup. The helper refuses to transfer a conflicted echo and never derives trust from raw echo data.

Use the same merge contract for live dispatch and history/replay paths. Existing `normalizeEvent` remains the sole trust gate. No Hub change is needed, and no provenance is inferred when the canonical event is missing or fails the existing trust checks.

## Validation

- Reproduce with Hub SSE IDs for `input_posted`, `delivery_update(cli_injected)`, and the admitted `user_input` echo.
- Record icon count and accessible label in waiting, post-injection, and final admitted DOM states.
- Verify the final DOM contains one input card, carries the admitted turn/body, and retains the canonical Agent icon.
- Negative controls: absent provenance, non-trusted provenance, non-Hub attestation, and a different delivery must not produce or borrow an icon.
- Verify the SSE identity/order metadata survives the merge and the result is stable on history replay.
