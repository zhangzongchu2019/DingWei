# Helper self-update Hub integration note

The sole authoritative packet contract is
`docs/reviews/coder-helper-self-update/RUNTIME-CONTRACT.md` at
`review/coder/helper-self-update@e9e5f3f86c8f4e029d974f0bdc07680a21a5e2f9`.
Fetch that exact Git object for health, receipt, confirmation, lock, and
activation semantics. This note deliberately does not restate those packet
rules; it only fixes Hub API names and state ownership.

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

`provision_ack` records ordered lifecycle stages. Its packet fields and all
health/confirmation semantics are defined only by the authoritative contract
above.
