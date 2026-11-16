# S-VIEW-AGENT-ICON-MISSING read-only findings

## A. Exact disappearance path

The Hub path trusts the incoming Agent source: `internal/m8/hub.go:1421-1433` strips sender claims and stamps authenticated `agent_session`; `internal/m8/terminal.go:855-883,896-922` includes trusted provenance on `input_posted` for a parseable Agent session sender. The renderer displays it only with the icon flag enabled and Hub-attested trusted provenance: `view-service/renderer-canary-sse.html:2223-2271,2366-2384`.

Observed sequence: icon present in the waiting card; still present after `delivery_update(cli_injected)` renders the original canonical event (`2602-2622`); absent after the same-delivery admitted `user_input` takes over the turn input (`2719-2745`, then turn rendering). The cause is loss of canonical provenance at the admitted-echo merge, not absent Hub provenance and not the separate waiting-entry text replacement at `2818-2819`.

## B. Earlier renderer history

C22 `c22bb63fb190ca62fe121bb88856e13a84b1e270` contains the same provenance icon gate (`renderer-canary-sse.html:2366`) and the same admitted-card replacement (`2730`, `3854`), so this renderer behavior predates C22. The available R57 integration source also contains both the icon renderer (`2236`) and waiting-event replacement (`2643`), consistent with the same class of issue in that candidate source. The requested short hash `cc8142b0` is not present in the available object database and `git fetch origin cc8142b0` cannot resolve it; therefore that exact artifact cannot be independently compared. The available files establish source history, not that a specific R57 build was live.

## C. Minimal SSE-ID reproduction

A Playwright fixture used the current renderer with `PROVENANCE_ICONS_V1=true` and these sequenced events: `input_posted` at Hub epoch `abcdef0123456789`, seq 1, message `msg-agent-1`, delivery `dw-dlv-agent-1`, body `探针正文`, and Hub-attested Agent provenance for `qwen38mvb4`; `delivery_update(cli_injected)` at seq 2 for the same delivery and turn `turn-agent-1`; `user_input(admitted)` at seq 3 for the same delivery and turn, with the same body and no message ID/provenance (matching the existing asymmetric helper echo shape).

| Boundary | waiting rows | timeline Agent icons | Accessible source label | Rendered content |
|---|---:|---:|---|---|
| After seq 1 | 1 | 0 | `来源:qwen38mvb4` on waiting row | `⏳ 投递中` and `探针正文` |
| After seq 2 | 0 | 1 | `来源:qwen38mvb4` | `探针正文` with delivery status |
| After seq 3 and the 800 ms echo hold | 0 | 0 | none | One turn-group input with `探针正文`; header contains time and diagnostic `i` only |

The fixture used valid SSE IDs `abcdef0123456789-1`, `abcdef0123456789-2`, and `abcdef0123456789-3`, then passed the same epoch/sequence and `hub:abcdef0123456789` timeline rank through the renderer dispatch boundary. The final turn group retained `data-timeline-seq=3` and the admitted turn/body, but had no `.prov-ic`; the canonical event before admission had one.

## D. Proposal

The OpenSpec change proposes retaining only the correlated canonical event's already-normalized, Hub-attested provenance while using the admitted echo for body, turn, timestamp, and SSE ordering. It applies at live and replay merge paths, keeps existing fail-closed checks, and makes no Hub changes. Strict validation: `npm exec --yes --package=@fission-ai/openspec@1.16.0 -- openspec validate view-agent-icon-missing --strict` returned `Change 'view-agent-icon-missing' is valid` with exit code 0.
