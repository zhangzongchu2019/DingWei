# Existing incident evidence and CLAUDE comparison

This supplement incorporates the leader's complete existing evidence supplied after d67c44216bf7216854f5bc8963f91c7059a85d41. No new production sampling, deployment, live message or full-gate rerun. Product source is unchanged from d67c4421.

## Accepted observations and limits

Owner report at 2026-11-01T05:02:17Z: the ts3opus55.zzc View status bar changed from one item on disk/not injected to one item on disk/injected; the newly submitted Console/View input card showed no submission/delivery/injection/echo status. The report establishes the visible symptom; it does not identify the displayed card's message ID or record its event sequence.

prodops read-only snapshot at 2026-11-01T05:04:47Z: the latest three deliveries all had ack_protocol=2, processing_mode=driver, state=processed, turn_outcome=completed, empty last_error, legacy_received=false, admission_unknown=false:

| delivery_id | source message_id | created UTC | delivered UTC | injected UTC | processed UTC |
| --- | --- | --- | --- | --- | --- |
| dw-dlv_a073022f53991cacfa695447539a0d75 | 57141700617d6e3cdf06a24bbb99eb1e | 05:04:10.886Z | 05:04:11.077Z | 05:04:14.855Z | 05:04:45.983Z |
| dw-dlv_cce9cd0461277a978369e65853196aae | dcb08183123b29b98571393ee46053b1 | 05:03:16.467Z | 05:03:16.634Z | 05:03:18.602Z | 05:03:37.573Z |
| dw-dlv_2ff330b5ed10f5de7b931c2ffc4860f1 | 3a1ea10189dc6000ea8fe203a4e02f5f | 04:58:55.226Z | 04:58:55.393Z | 04:58:56.869Z | 04:59:13.545Z |

All times above are on 2026-11-01. Envelope meta.delivery_id equals DB delivery_id; envelope.id and meta.source_message_id equal source_message_id. These show consistent backend delivery coordinates and completed processing. They do not prove the contents, presence, order or successful application of any input_posted/delivery_update/user_input/echo event in the browser. The latest three rows are not a recorded mapping of the exact card reported at05:02:17Z.

Hub journal had one durable_queued per delivery and zero delivery_update lines across the window beginning04:58:30Z. Delivery events publish via the in-memory eventRing/SSE rather than journal, so absence of journal entries proves neither publication nor non-publication. No SSE field summaries or retained events exist in the supplied materials. Do not infer presence or absence of delivery_id from the DB or journal.

Both ts3opus55 and the leader were CLAUDE seats with helper0.2.3.14; leader's card status was normal. ts3opus55 capabilities/connection observation: delivery_ack_v1=1, conn_seq=5, active=1, last_seen05:04:45Z. The Codex-specific hypothesis is withdrawn. The capability observation does not override the explicit per-delivery ACK2 records. Version strings do not bind either deployed helper or renderer to an exact source commit.

## Source and local comparisons

Historical reference edaf40e60ac522decedefee56affe861d0545aaf pins helper0.2.3.14. Its ClaudeProtocolDriver._handle_frame assistant branch emits assistant_text with active.envelope_id and active.turn_id. claude-014-fixture.py executes that exact function with a synthetic active turn and a synthetic assistant frame; no constructor, ledger, CLI or network starts. Generated event: raw/claude-014-fixture.json. This is a historical source-path witness, not a claim that either deployed binary matched that commit or that the incident emitted that event.

field-comparison.js serves the actual pre-fix3fd26ba6 and fixed renderer in Chrome. It feeds the historical CLAUDE projection and synthetic canonical delivery events using the three supplied coordinate pairs. Synthetic content and turn/session IDs remain explicitly synthetic. Two pages in the same browser context share origin/session storage facilities while maintaining separate renderer maps and cursors.

| Comparison | Pre-fix renderer | Fixed renderer | What this establishes |
| --- | --- | --- | --- |
| Same session/page A: primary then linked CLAUDE output then ACK updates | Empty status, no card index, updates stranded under the exact source message ID | Correct same-message completed status | Reproduced race is driver independent and applies to the historical CLAUDE projection |
| Same session/page B: primary then ACK injected then CLAUDE output then final ACK | Correct completed status | Correct completed status | Different event order alone explains why one synthetic page is normal while another fails; not evidence of actual multiple tabs |
| Other two supplied coordinate pairs, early output order | Same missing-index failure | Correct completed status under exact supplied message key | Backend-coordinate consistency alone does not prevent renderer index loss |
| Sequenced duplicate replay and after cursor | Duplicate rejected; after=aa-2 retained in first page | Same | The real renderer admitEventId/eventsStreamUrl functions behave consistently in this synthetic history |
| Retained suffix contains output/final update but no input_posted | No canonical status card reconstructed | Same | The patch cannot recover absent primary identity/body; it does not fabricate missing events or infer identity from text |

Final shared-context run:16 assertions,0 failures. raw/field-comparison.log includes all state snapshots and explicitly labels the scope. An earlier separate-browser-context run is also preserved as raw/field-comparison-separate-contexts.log. These runs exercise real renderer cursor functions with synthetic sequenced inputs; they do **not** exercise an actual network reconnect or verify a browser-sent Last-Event-ID header. Source review at terminal.go:980–987 shows explicit after query takes precedence over Last-Event-ID fallback. Renderer cursor and card maps are document-local; no shared localStorage map for these identifiers was found. Parent Console mux feed, if enabled in the incident, has different transport ownership and is not proven by this standalone View comparison.

## Disposition

The existing3-line patch remains justified by a deterministic, CLAUDE-applicable local race and is not expanded. No helper-specific ID injection or text/time-based card merge is warranted by these materials. Original field causation remains **unconfirmed** because event content/order, reconnect/Last-Event-ID state, renderer source binding and the actual number/type of open View pages are unavailable. No further production sampling was requested or performed.

The earlier full gate belongs to d67c4421's unchanged product/test source: rc0,2486Python unittest/0fail,2shell pass; related m8 top-level201pass/5skip/0fail. This supplement only adds evidence/probes and runs the supplemental Chrome comparison. It does not claim a second full gate or repeat the original R45 skip gate.
