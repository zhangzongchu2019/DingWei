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


## E. Renderer-only implementation and verification

- Root-cause confirmation: replaying the same three sequenced events at every renderer point found the first icon-count change from 1 to 0 at `b425568ef3e1d4690de8b37327c28835b7eaaa3d`; `3184933e` was not the introducing commit.
- Implementation: `view-service/renderer-canary-sse.html` stores a normalized provenance snapshot only for a primary canonical input whose existing trust checks passed. Both the live admitted-echo merge and the history/replay merge copy only that snapshot after exact delivery-index matching. The admitted echo continues to supply its text, turn, timestamp, and sequence. No Hub code changed.
- Regression: `renderer-agent-icon-admission-regression.js`, registered in the full runner, sends Hub-sequenced frames with IDs `abcdef0123456789-1` through `-3`. The 9/9 check groups cover Agent and timer sender names, post-injection and admitted display, replay, absent/untrusted/non-Hub-attested provenance, wrong delivery, admitted body/turn, and preserved timeline sequence.
- Mutation: temporarily removing the provenance retention helper made the live Agent icon, timer icon, and history/replay icon business assertions fail (rc=2); restoring the implementation returns the new suite to 9/9.
- Full browser runner at `18b50c1a93b7e92b299eb53c613da497e612838f`: 44/44 suites rc=0, aggregate rc=0. The same full runner at a0 baseline `d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e`: 43/43 suites rc=0. All 43 shared suites remained green, including `renderer_epoch_order`, `renderer_timeline_order`, and the history mutation suite. The new renderer SHA256 is `8cabb3514df3f381aeb84662af3a271216519f143a05cebf10ec1c08d700e85c`.
- OpenSpec 1.16.0 strict validation after merging this change returned `Change 'view-agent-icon-missing' is valid` (rc=0).
- Repository build gate: `scripts/run-build-tests.sh` exits 1 before Python/shell tests because helper discovery reports `test_desktop_endpoint`, `test_guide_staging`, `test_ipc_socket_path_isolation`, and `test_sessionhelper` without collected cases. Running the same helper gate at the unchanged d2 baseline reports the same four modules. This task makes no helper changes.
