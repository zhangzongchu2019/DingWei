# Helper runtime integration — authoritative wire contract

Frozen design fc3e4b45; does not change its authorization or state contract.

New helper version0.2.3.1. Stable mounted source guard gates candidates; old overwrite-in-place update_self is bypassed by SessionHelper.handle_provision. Shared-key/no launcher installs report manual_required. Existing non-update install_skill/install_mcp handling remains unchanged (public-upload retirement is Hub work).

Authoritative wire, implemented by the separately owned helper and Hub components: existing trusted provision envelope action=update_self,target=self; job_id/release_id/agent_id/owner_id/agent_name/version/sha256/lock_sha256/platform=linux/expires_at(RFC3339)/url and Hub-configured file_service_origin. Agent name is compared with dedicated credential. The six canonical job fields are frozen in the guard journal; the verified job/lock binding is retained in private slot metadata. File-service origin is accepted only after existing local-Hub source validation; HTTPS, exact origin, no redirects/proxy/credential headers.

Hub WebSocket response feature header X-DW-Helper-Update: helper-update/v1 gates new control frames; session_capabilities.helper_update_v1 advertises dedicated Agent+stable launcher+Linux. On fresh authenticated reconnect, healthy candidate sends helper_update_health with tuple and fresh health_nonce; trusted helper_update_confirm must echo nonce/tuple and current compiled version. Driver/IPC readiness checked before sending; no pending job means no health deadline/confirmation. Old Hub receives no new control subtype. This wire must be matched with actual Hub commit before combined E2E/acceptance.

Receipts use provision_ack with frozen tuple, stage/from_version/to_version/reason. Idle adapter_handle_lock prevents update while a model call holds it; idle is rechecked after acquisition. Downloaded/installed receipts precede restart request. Duplicate installed job never restarts twice. Guardian confirmation moves pending to current, retains previous.

Executed local targeted matrix:16 core +8 actual HTTP/archive offer checks +4 actual app-dispatch/old-Hub control checks =28 rc0. Existing receive-quiescence/reconnect plus core/offer matrix35 rc0; the first invocation failed5 extraction tests because a new classifier global was unavailable in their source-extraction namespace, corrected by self-contained type inspection (no old assertion relaxed); original invocation retained in session output. No claim of full final suite, actual Hub or Docker acceptance yet. Initial core full660/canonical42+2 evidence remains separately scoped to earlier core snapshot.

## Follow-up before fixed-snapshot full gates

Canonical VERSION now also0.2.3.1; existing setup/version25 assertions pass without weakening. Actual enrolled helpers use placeholder local key IDs, so update-only validate_source pins the exact system From through authenticated X-DW-Helper-Update-Source response header (same feature gate). No inferred owner suffix/wildcard; non-update source validation unchanged. Hub author must implement this exact handshake/confirmation wire before combined acceptance.

Guardian child preserves original business cwd using a stdlib runpy trampoline and explicit selected import root, avoiding driver/A6 default-workdir drift and stable-root recursion. Auxiliary MCP selects the already-running pending slot without charging its budget. Frozen tuple includes owner_id. Lost installed receipt before restart marking can re-request restart without download/reinstall; after durable restart marking duplicates do not trigger a second restart.

Latest targeted32 core/offer/app tests plus setup/receive/old-provision regression74 rc0; invariants/MCP wiring+new core/offer56 rc0. New environment accesses use env_get and test constructor uses module HOME isolation. Initial intermediate full675 run rc1 (env-read/home-isolation violations plus inspect source-location check while source was edited); initial canonical runtime gate rc1 (VERSION mismatch). Retain these failed logs. They are not a final passing full/gate claim. Freeze current code and rerun both before final receipt.

## Boundary follow-up

The exact11ae56d fixed snapshot passed full679 in347.732s and canonical42+2; receipts and source pins are in FIXED-RUNTIME-RECEIPT.json. Subsequent bounded fixes recheck offer expiry after download and before arming, normalize legitimate parent HOME aliases to the same private state path in preflight/guard (a symlink at the update root remains rejected), and retain original from_version through candidate confirmation/rollback rather than reporting new->new. Latest targeted34 checks pass; expiry revalidation withdrawal fails1. Full/gate rerun for this follow-up is pending; no inherited exact-head full PASS claim. No actual Hub/Docker/production acceptance.

## Helper bounded health/receipt alignment (reviewer joint direction)

Health uses a new helper-generated nonce for each attempt and includes the SHA256
of the immutable mounted `requirements-runtime.lock`. A pending healthy trial
retries every five seconds so a dropped confirmation does not strand it; a new
connection creates a new status loop and nonce over the same canonical job.
Confirmation must pass trusted-Hub source validation, match the latest nonce,
all six frozen job fields and runtime version, and echo the same lock hash; the
mounted lock must still have that hash. Receipts include lock_sha256; rollback
receipts derive it from that same mounted lock. No guard journal format change.

This file is the sole authoritative wire contract. The Hub implementation must ensure health arrives
on a connection strictly later than the install/activation boundary (not merely
offer issuance), only eligible post-install or idempotently confirmed identical
jobs qualify, and offered/downloaded/failed/rolled_back/stale jobs do not. A valid
retry of an already-confirmed job echoes the current helper nonce without another
mutation, audit or activation. These are Hub obligations and are not proved by
the local helper tests. Actual serializer-to-Hub and real Docker gates remain open.

## Leader R3 binding amendment (2026-10-15; rulings e9a57e25a713, 4ae89d80c704, c2035f878038; supersedes any conflicting text above or in any Hub-side wire document)

1. **Nonce.** `helper_update_health` carries a fresh `health_nonce` generated by the helper on every health send. The Hub echoes that exact nonce in `helper_update_confirm`. The Hub never stores, issues or compares an offer-time nonce.
2. **Lock hash.** `helper_update_health` carries `lock_sha256` computed from the mounted, immutable `requirements-runtime.lock` at send time. If the file is unreadable or its hash differs from the job's `lock_sha256`, the helper fails closed with `lock_changed` and never substitutes a value. The Hub compares it with the job's stored `lock_sha256` and echoes it in `helper_update_confirm`; a mismatch is rejected.
3. **Freshness boundary.** Confirmation is accepted only on an authenticated connection established after the job reached `installed` or `restarted`; the Hub records the connection sequence at that stage transition and requires a strictly later one. Jobs in `offered` or `downloaded` are never confirmed (including the first connection after an offline offer).
4. **Idempotent confirmed replay.** For the same canonical job already `confirmed`, a valid repeated health (same tuple and lock, latest nonce) receives an idempotent `helper_update_confirm` echo with no change to stage, revision, audit or activation. `failed`, `rolled_back` and `expired` are terminal and never resurrect.
5. **Capability on every send.** Every offer send and replay, including reconnect resume, requires the current authenticated connection to have advertised `helper_update_v1`.

Required tests: offline issuance then first connection refused; reconnect before install refused; installed → new connection → confirmed; lost confirm → retried health → idempotent echo without side effects; stale/changed nonce, tuple or lock rejected; failed and rolled_back refused; legacy reconnect without capability receives no offer; real helper serializer → Hub.

### Helper send-time job-lock enforcement

After verifying the release, prepare fsyncs `.helper-update-binding.json` inside
the new private slot before arming it. It binds the six canonical job fields and
the verified lock hash. After process restart, health compares that binding to
the durable pending tuple and the immutable mounted lock; missing, changed or
wrong-job binding fails closed with `lock_changed`. Confirm repeats the binding
check. The marker is runtime private state, not part of the exported package,
and the stable guard journal remains unchanged.

## Leader R3 clarification — activation boundary and lost confirmation

This section incorporates the latest accepted reviewer clarification and is
binding on both implementations. It refines the Leader R3 amendment above; it
does not change the frozen design or claim any implementation/test closure.
Hub-side `docs/designs/helper-self-update-wire.md` must match or point to this
file; it is not an alternative contract.

1. **Connection boundary is installation/activation, never issuance.** A health
   confirmation requires an authenticated connection established strictly after
   the job's recorded installation/activation boundary. An offline-issued offer
   may first arrive on a later connection, but that first delivery connection
   is still pre-restart and cannot confirm. Confirmation is refused while the
   job is `offered` or `downloaded`, on the activation connection itself, and
   for `failed`, `rolled_back`, expired or otherwise stale jobs. Replaying an
   offer cannot move or bypass the activation boundary.
2. **Dropped confirmation is recoverable without another transition.** For the
   same canonical job already `confirmed`, a valid retried health, including a
   new helper nonce after reconnect, receives an idempotent confirmation echo.
   Agent/owner/job/release/version/package SHA/lock SHA and applicable connection
   and expiry checks still apply. The Hub echoes that health's current nonce;
   it does not compare it with an offer nonce. There is no repeated stage write,
   revision increment, audit event or activation. `failed` and `rolled_back`
   never resurrect.

Required combined matrix (not replaced by separate local helper/core tests):

- Drop the first confirmation, retry health on the live connection and after
  reconnect; the same confirmed canonical job receives the current nonce echo
  without any additional transition/revision/audit/activation.
- Reject stale or changed confirmation nonce, changed/stale canonical tuple or
  lock, and expired jobs. A newly generated helper nonce on a valid retry is
  expected, rather than an offer-nonce mismatch.
- Refuse confirmation on the same pre-activation socket and after offline
  issuance on the first offer-delivery connection; refuse offered/downloaded.
- Refuse failed/rolled_back jobs, retaining terminal state and side effects.
- Feed JSON produced by the real helper serializer into the authenticated Hub
  path, including its runtime lock hash, rather than a hand-built substitute.

Ownership remains unchanged: togpt56 implements and tests Hub freshness, terminal
state and idempotency; coder implements helper lock serialization/verification
and the combined E2E fixture. The local helper receipt at source8f2adda2 does
not prove these Hub or full-chain gates.

## Actual helper packet fixtures and lock negatives

Tracked samples are in `wire-packets/PACKETS.json` beside this contract. They are
generated by `wire-packets/capture.py` using the actual helper
`SessionHelper.handle_helper_update`, `_send_update_receipt`, `status_loop`,
`SelfUpdater.confirm`, and `handle_update_confirmation`. They include real
serialized `downloaded`/`installed`/`confirmed` provision_ack and health envelopes.
The confirm input is explicitly a **synthetic trusted Hub echo**, not evidence of
a real Hub; the rollback packet uses actual status_loop after a controlled guard
failure in a separate fixture/job, not resurrection of the confirmed job. The
source/archive HTTP is local loopback, synthetic candidate version0.2.3.2.
No live Agent, real Docker, or production connection is involved.

- `health.meta`: six canonical fields plus fresh `health_nonce`, mounted
  `lock_sha256`, `action=update_self`, `stage=restarted`, runtime/from versions.
- Normal receipts carry the validated offer lock; the confirmed receipt carries
  the accepted echoed lock. Rollback status reads the same immutable runtime
  lock under the no-lock-change invariant. It never substitutes a different
  hash to satisfy the Hub.
- `negative_confirmations.missing_lock` and `.wrong_lock` contain the actual
  rejected confirm inputs and receiver result `confirmation_mismatch`; the
  actual app dispatch emits no receipt and preserves pending state.
- Missing runtime lock and changed runtime lock after reopen both produce
  `lock_changed` before health. Missing/wrong-job/changed slot binding does too.

`PacketFixtureTests.test_actual_serializers_and_missing_wrong_lock_confirmations`
regenerates/asserts these semantics against actual helper code; IDs, timestamps,
nonces, artifact hashes and loopback ports are per-run fixture values. This is a
helper wire contract/test, not closure of the required real serializer-to-Hub
combined gate. The established leader R3 amendment and activation/idempotence
clarification above remain binding on the Hub implementation.
