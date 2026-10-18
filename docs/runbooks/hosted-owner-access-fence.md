# Hosted human access fence (S-HOSTED-OWNER-FENCE)

Base r27. This change supplies the server-side identity fence required before the first owner-only hosted trial. It does not authorize enabling hosted services, changing production configuration, restarting the Hub, logging into an LLM account, or starting a trial.

## Configuration and execution-list prerequisite

Set `DW_G4_ACCESS_ALLOW_USER_IDS` in the Hub's private service configuration to the owner's **canonical user ID** from the current identity user record, for example `owner`. This is not a display name, short name, role, Agent full name, or a credential. Multiple IDs are comma-separated; surrounding whitespace is trimmed, duplicate IDs are harmless. IDs must be lowercase ASCII alphanumeric, 2–32 characters. Empty tokens, uppercase IDs, wildcards and malformed IDs reject hosted assembly with a generic configuration error. Do not copy any credential into this setting or into evidence.

**Unset, empty or whitespace-only means no human may use hosted services.** No role, default quota, verified combo, previous session or future registration opens it. The master-key/configuration prerequisites remain separate; without hosted assembly, routes remain absent. A configured hosted assembly with an empty list returns uniform `403 {"error":"forbidden"}` for human routes. The independently authenticated registered prodops GET monitor exception below remains available.

prodops must add this explicit setting and the tests below to the enabling runbook; the owner must approve the actual IDs and restart separately. Retain other users' zero hosted quotas and the operational freeze on platform user activation/quota changes as additional rollout controls. This change does not globally disable registration, ordinary Console access, or non-hosted user activation: a newly registered/activated user still cannot enter hosted services unless explicitly listed. Default hosted quota is not an access grant.

## Identity, sessions, reload and routes

The list is parsed once during Hub startup and copied into immutable route/page policy. Editing the environment/service file requires a Hub restart; it is not a live reload. Removing an ID takes effect for all old sessions after that restart. No session recreation is required to grant a currently valid listed identity. Every request resolves the real N3 cookie session against current identity state; expiration, revocation, disabled users and mandatory password change deny immediately. Role changes never add list membership. Listed users still need existing owner/manager/resource ownership/Origin/hold-generation permissions; the list grants no additional business privilege.

Any Authorization header is ineligible for human access, including an Agent bearer combined with an owner cookie. Identity/role/forwarded-user headers are ignored. The human fence runs before body decoding, resource lookup or business/audit side effects and gives the same no-store 403 for missing and existing resources.

Covered: hosted options/create/job progress/all lifecycle operations; quota list/edit; personal and administrator LLM account routes; login start/status/cancel; human quota-monitor; hold release and quota page. Generic Agent listing remains available but does not call the hosted provider or inject hosted metadata for fenced identities. The ordinary Console is still available; its per-request hosted feature flag hides creation, account, quota and lifecycle controls. API enforcement is authoritative even for a previously loaded page or a hand-crafted request.

The existing **exact registered prodops Agent credential GET `/api/console/hosted/quota-monitor`** remains read-only under its original authentication rules (including owner+ops configuration and agent credential validity). It works independently of the human list, cannot release, cannot open the human quota page, and cannot read private material. Node connect, backup upload and login upload keep their separate node authenticators; this human policy does not intercept those machine routes.

## Local verification and operational acceptance (execution separately approved)

Local tests cover each human route against real sessions; owner positive business paths; outside manager/platform_admin/member; empty/unset/invalid configuration; registration and activation; old-session disable/revocation/expiration; forged headers/Agent bearer; configured assembly and per-request page rendering; hosted metadata suppression; unchanged read-only ops boundary. The browser fixture checks assembled-but-denied UI; the Go page test separately checks absent assembly. Source withdrawal must break the corresponding unchanged assertions. Full canonical remains 49 Python + 2 shell entries, including local temporary PG; hosted/workpulse full tests use a 45-minute timeout.

Before an authorized rollout, verify the actual owner ID, current service environment, no other allowlisted identities, direct/API denial from an outside active user and manager/admin, owner-only account login exposure, fresh/old session behavior, and prodops monitor-only access. A visible hidden control is not evidence of denial; record actual server status. No actual environment or rollout acceptance is claimed by local tests.

Rollback: separately authorized restore of the prior executable/configuration; retain the external exposure fence while doing so because the prior binary lacks this server fence. Setting the new allowlist empty and restarting closes human hosted access on this implementation without changing data, credentials, quotas or Agent state. Do not automatically reopen routes, activate users or start/stop Agents.
