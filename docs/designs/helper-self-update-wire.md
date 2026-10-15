# Helper self-update wire contract (Hub section)

This document fixes implementation names only; it does not change the frozen
DW123 design.

## Core API

Console adapters must derive `principalID` from the authenticated server-side
session and call:

- `IssueHelperUpdate(ctx, principalID, agentID, releaseSHA)`
- `ListHelperUpdateCatalog(ctx)`
- `GetHelperUpdateJob(ctx, principalID, agentID)`

`IssueHelperUpdate` rechecks that the active Agent and its active owner belong
to the active principal. `GetHelperUpdateJob` repeats the owner check. The
transitional `IssueHelperUpdateForActor` name is not a Console contract.

## Catalog and offer

An operations-host CLI registers an immutable catalog item with `sha256`,
`lock_sha256`, `version`, `url`, `file_service_origin`, and `platform`. The
Hub validates that the URL has the exact registered origin. A durable job
carries that same origin. Its trusted `provision`/`update_self` envelope
contains `job_id`, `offer_id`, `release_id`, `agent_id`, `owner_id`,
`agent_name`, `version`, `sha256`, `lock_sha256`, `url`,
`file_service_origin`, `platform`, `issuer`, `expires_at`, and `health_nonce`.

The authenticated Agent WebSocket advertises `X-DW-Helper-Update:
helper-update/v1` and `X-DW-Helper-Update-Source`. A connected helper must
first send `session_capabilities.helper_update_v1=true`; otherwise the Hub
returns `helper_update_manual_required` and creates no offer.

`provision_ack` records only ordered lifecycle stages. A
`helper_update_health` must echo `job_id`, `release_id`, `agent_id`,
`owner_id`, `version`, and `sha256`, plus a fresh helper-generated
`health_nonce`. It does not carry `lock_sha256`. It is accepted only on an
authenticated connection with a strictly later Hub connection sequence than
the connection that first received the offer, reporting the offered version.
A replayed offer never resets that boundary. Failed, rolled-back, and
confirmed jobs are terminal and cannot be confirmed again. The Hub responds
with trusted `helper_update_confirm` echoing the tuple and that fresh nonce.
