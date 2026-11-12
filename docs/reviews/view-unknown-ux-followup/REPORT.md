# S-VIEW-UNKNOWN-UX-FOLLOWUP review record

## Scope and decision

Base is frozen `c01c2f40c02e55c8f37b860ae78753a915c1d725`; implementation is limited to `view-service/renderer-canary-sse.html` and `view-service/renderer-delivery-status-regression.js`. No Hub/helper/version or browser-suite inventory changes.

The unknown list computes a render signature only from visible row identity and rendered content/expiry. The helper aggregate is rendered independently, so aggregate-only changes update the note without replacing list nodes. Identical timer refreshes return without touching the list DOM. The summary count is the number of visible local detail rows. When helper aggregate exceeds that count, a separate note reports both counts. “不再跟踪” therefore decrements the summary and row count while the helper aggregate note remains explicit.

## `deliveryByTurn` scenario review

**Finding: no reliable real scenario warrants retaining the fallback.** In the frozen page, `deliveryByTurn` was populated only by a `delivery_update` at stage `cli_injected` carrying both turn and delivery IDs, then used only as fallback when a later event had no delivery/origin delivery ID. `cli_injected` is the point at which the corresponding item has left the waiting/unknown list, so the fallback cannot remove a retry control from a currently visible row for that same delivery. The direct-ID paths already handle `turn_processed`, `mention_reply`, and `assistant_text` when those events carry the delivery ID. Hub mention-reply summaries deliberately omit turn and delivery IDs (`internal/m8/view_mention_reply.go`, line 289 in the inspected base), so a turn-only association is not a valid way to attribute those summaries. Inference: because the renderer has no trustworthy correlation key for such an ID-less summary and the retry row is already gone after `cli_injected`, the map does not cover a useful visible retry-state case and risks associating an unrelated event by turn. It was removed; event handling now uses only the direct delivery/origin ID.

Baseline locations: `deliveryByTurn` declaration at frozen renderer line 1757; map population and fallback at lines 2489–2490. Current renderer retains direct-ID removal at lines 2500–2505. These are code observations; statements about lack of a useful user-visible case are the inference above.

## Regression evidence

The browser regression now injects a deterministic clock and checks:

- `UNKNOWN_LIST_STABLE_NODE_AND_FOCUS`: advancing the clock by three seconds without events preserves the exact button DOM node and focus.
- `UNKNOWN_SUMMARY_MATCHES_VISIBLE_ROWS`: with helper count 5 and two local rows, summary/list show 2 and the separate helper note states 5 versus 2; after untracking one item, both become 1 while the note states 5 versus 1.
- Existing `UNTRACK_IS_LOCAL_ONLY` checks no request was sent.

Semantic mutations are stored under `mutations/`: removing the render-signature guard fails at `UNKNOWN_LIST_STABLE_NODE_AND_FOCUS` (`rc=2`); making the summary use the helper aggregate fails at `UNKNOWN_SUMMARY_MATCHES_VISIBLE_ROWS` and the untrack count assertion (`rc=2`).

## Gate readings

Final gate readings and UTC intervals are in `readings.txt`; the full machine receipt is `browser-receipt.txt`. `run-build-tests` returned `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2` using the locked venv and clean environment. The runner selected all 40 suites and returned `aggregate_rc=0` with its process on Node 22.23.1. `mcp_zero_consent` internally launches `/usr/bin/node` because its existing fixture fixes PATH; it uses Playwright 1.55.0, which supports that Node 18.19.1 child. This is an existing harness environment boundary; no suite file was changed. Added HTML lines were checked for version literals; none were added.
