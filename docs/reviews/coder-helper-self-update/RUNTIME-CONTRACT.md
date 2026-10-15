# Helper runtime integration — implementation wire proposal

Frozen design fc3e4b45; does not change its authorization or state contract.

New helper version0.2.3.1. Stable mounted source guard gates candidates; old overwrite-in-place update_self is bypassed by SessionHelper.handle_provision. Shared-key/no launcher installs report manual_required. Existing non-update install_skill/install_mcp handling remains unchanged (public-upload retirement is Hub work).

Current wire proposal sent to Hub author for alignment: existing trusted provision envelope action=update_self,target=self; job_id/release_id/agent_id/owner_id/agent_name/version/sha256/lock_sha256/platform=linux/expires_at(RFC3339)/url and Hub-configured file_service_origin. Agent name is compared with dedicated credential. SHA/hash/IDs are frozen in journal. File-service origin is accepted only after existing local-Hub source validation; HTTPS, exact origin, no redirects/proxy/credential headers.

Hub WebSocket response feature header X-DW-Helper-Update: helper-update/v1 gates new control frames; session_capabilities.helper_update_v1 advertises dedicated Agent+stable launcher+Linux. On fresh authenticated reconnect, healthy candidate sends helper_update_health with tuple and fresh health_nonce; trusted helper_update_confirm must echo nonce/tuple and current compiled version. Driver/IPC readiness checked before sending; no pending job means no health deadline/confirmation. Old Hub receives no new control subtype. This wire must be matched with actual Hub commit before combined E2E/acceptance.

Receipts use provision_ack with frozen tuple, stage/from_version/to_version/reason. Idle adapter_handle_lock prevents update while a model call holds it; idle is rechecked after acquisition. Downloaded/installed receipts precede restart request. Duplicate installed job never restarts twice. Guardian confirmation moves pending to current, retains previous.

Executed local targeted matrix:16 core +8 actual HTTP/archive offer checks +4 actual app-dispatch/old-Hub control checks =28 rc0. Existing receive-quiescence/reconnect plus core/offer matrix35 rc0; the first invocation failed5 extraction tests because a new classifier global was unavailable in their source-extraction namespace, corrected by self-contained type inspection (no old assertion relaxed); original invocation retained in session output. No claim of full final suite, actual Hub or Docker acceptance yet. Initial core full660/canonical42+2 evidence remains separately scoped to earlier core snapshot.

## Follow-up before fixed-snapshot full gates

Canonical VERSION now also0.2.3.1; existing setup/version25 assertions pass without weakening. Actual enrolled helpers use placeholder local key IDs, so update-only validate_source pins the exact system From through authenticated X-DW-Helper-Update-Source response header (same feature gate). No inferred owner suffix/wildcard; non-update source validation unchanged. Hub author must implement this exact handshake/confirmation wire before combined acceptance.

Guardian child preserves original business cwd using a stdlib runpy trampoline and explicit selected import root, avoiding driver/A6 default-workdir drift and stable-root recursion. Auxiliary MCP selects the already-running pending slot without charging its budget. Frozen tuple includes owner_id. Lost installed receipt before restart marking can re-request restart without download/reinstall; after durable restart marking duplicates do not trigger a second restart.

Latest targeted32 core/offer/app tests plus setup/receive/old-provision regression74 rc0; invariants/MCP wiring+new core/offer56 rc0. New environment accesses use env_get and test constructor uses module HOME isolation. Initial intermediate full675 run rc1 (env-read/home-isolation violations plus inspect source-location check while source was edited); initial canonical runtime gate rc1 (VERSION mismatch). Retain these failed logs. They are not a final passing full/gate claim. Freeze current code and rerun both before final receipt.
