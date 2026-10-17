# H1 hosted restricted MCP sending — 0.2.3.4

Base r19 `8a3daeafddaf0fa2548127889b255be7528b6226`; ref `review/coder/hosted-mcp-send`. Frozen product/test commit `8e0b9e3728d183bf5498525b3c20968917e8a6c0`, tree `fc22541e7d9f5c3f6e8c7aaacc7540714416d3bc`; this delivery adds evidence only afterward. No H2/H3 image/backup changes or H4 build-loop changes are mixed into H1.

`SH_IPC_SEND_BODY_ONLY=1` explicitly enables descriptor-policy uploads and inline-body mcp_send. Default full IPC and legacy upload-only remain; if both flags are true upload-only is stricter and wins. Bad new-flag values fail configuration without echoing input. The helper listener rejects file fields even empty, send descriptors, unknown fields, vsapi/file reads/spec/inbox and all unrelated operations before dispatch. UTF8 bodies are bounded64KiB bytes; only this opt-in path gets enough framing capacity for JSON overhead, preserving the legacy framing limit. Unknown/malformed inputs and send failures use fixed categories. There is no independent connection/auth path or automatic resend.

The MCP shim must also receive the flag: it rejects file arguments before local reading, exposes only send_message/upload_file and rejects other direct tools/call. This matters because the existing general-purpose shim reads a file locally before converting it to body. H3 emits the matching explicit helper/shim flags/root/state/socket; it is separately reviewed. This transport boundary is not an OS sandbox against same-UID code independently reading files. Normal body messages call the original authenticated WSS sender, so Hub routing/group checks remain authoritative. Socket sent=true remains write acceptance, not delivery proof.

Version0.2.3.4 distinguishes changed runtime/permissions from immutable reviewed033 packages. It is a compatible opt-in security/hosted-restoration repair with unchanged A/B protocol fields and original four-pin lock SHA5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0. Root VERSION, Go constant, helper version and release-pinning test move together. No dependencies change.

Final local results on the frozen runtime:

| Gate | Result |
|---|---|
| Helper full from repo root |729 tests, rc0 |
| Same helper suite from helper cwd |729 tests, rc0 |
| Scoped canonical (existing PG integration explicitly excluded) |47 Python +2 shell, rc0 |
| Go internal/version |rc0 |
| New restricted contract |11 tests included in each full run; separate target+IPC regressions29 tests rc0 |
| Real local in-memory identity/Hub + helper + socket + shim |positive routing and post-group-removal non-delivery PASS, included in scoped canonical |
| Source withdrawal controls |3 independently rc1 |

The real Hub fixture uses synthetic enrollment/state and scripted stdio calls, no deployed Hub/Agent/model/PG. It proves body routing, reply metadata, and group removal blocking actual delivery even though a socket-write receipt exists. Existing desktop/claim-memory canonical assertions remain. Source withdrawals remove listener restriction, shim file guard or app wiring separately in temporary copies with unchanged assertions; exact baseline/mutant SHA/edits/logs are retained. Public upload still works, credential-path upload remains refused, secret send paths are trapped before reading, FD sends are denied/closed, and a send failure returns sanitized isError with one call then same-process recovery. Existing033 transport-error tests remain in the full helper suite.

Test adaptations: synthetic factory/slot fixtures copy the shim's new stdlib permissions dependency; no version-selection/body/budget assertion is relaxed. The shim retains the original one-environment-entry rule rather than increasing the whitelist. The new Hub child gets the existing evidence-backed synthetic-HOME allowlist treatment. No default full/upload-only assertion is weakened. All final tests use a private outer HOME; earlier failures and one PG-scope execution deviation are fully qualified in ATTEMPT-SCOPE.md. No unqualified full canonical/all-attempts-green claim.

No deployed container/host/real key/Agent/productionPG activity, offer, job, source-install, release build, image build or publication. Existing test packaging creates only disposable fixture archives. All deployment/owner gates remain. Independent review is requested from opus50ts; this is implementation evidence, not self-review PASS.


## UTF-8 request identity follow-up

Restricted sender now validates request_id, destination and reply-origin strings for
UTF-8 encodability before any send side effect. Lone surrogate IDs previously sent
once then failed while encoding the response; they now return only invalid_request,
without echo, send or retry. Same real listener handles the next valid Unicode ID
and sends exactly once. Targeted restricted suite12 tests rc0; remove only this
validation and the unchanged new regression turns red. Raw evidence utf8-id/.
Prior729/full gate readings refer to the earlier source; no successor full-suite
claim. Leader/reviewer complete canonical readings remain independently attributed.
