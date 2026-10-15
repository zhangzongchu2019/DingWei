# Helper self-update Hub integration note

The authoritative packet contract is
`docs/reviews/coder-helper-self-update/RUNTIME-CONTRACT.md` on
`review/coder/helper-self-update`. That document's Leader R3 binding amendment
is authoritative over this Hub integration note; this file only fixes Hub API
names and state ownership.

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
	`file_service_origin`, `platform`, `issuer`, and `expires_at`.

The authenticated Agent WebSocket advertises `X-DW-Helper-Update:
helper-update/v1` and `X-DW-Helper-Update-Source`. A connected helper must
first send `session_capabilities.helper_update_v1=true`; otherwise the Hub
returns `helper_update_manual_required` and creates no offer.

`provision_ack` records only ordered lifecycle stages. The helper-generated
health nonce and mounted lock hash fields, plus receipt
and rollback serialization, are specified by the authoritative helper
contract. Hub records the authenticated connection sequence when the job first
reaches `installed` or `restarted`; only a strictly later authenticated
connection may confirm. `offered`, `downloaded`, `failed`, `rolled_back`, and
expired jobs are rejected. A valid retried health for an already confirmed
canonical job receives an idempotent confirmation echo without another state
transition, revision, audit event, or activation.
