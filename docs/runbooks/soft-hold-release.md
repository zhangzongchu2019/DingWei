# Soft quota: owner release, notifications and read-only monitoring

Development baseline: integration/spec5ff-r24 `f209edc0f0ab664541c2340bc7644b90c661d069`.
This document describes code and local verification, not host/configuration or
deployment authorization. The four-seat upgrade freeze remains in force.

## Identity configuration

The Hub operator must separately obtain approval for both nonsecret bindings:

* `DW_G4_HOLD_OWNER_USER_ID`: the actual owner's lowercase **user ID**, not their
  short name, Agent full name, Console role or an arbitrary manager. The current
  implementation also requires that account to be an active `platform_admin`.
* `DW_G4_QUOTA_OPS_AGENT_FULL_NAME`: prodops' exact enrolled Agent full name,
  including its real registered case and user suffix, without a `#...` address.

No identity is inferred from shared Agent ownership. Unset owner configuration
disables human release and the notification worker. Missing routes remain in the
durable outbox. The owner and prodops bindings must both be verified before a
real pilot; changing them or restarting the Hub is a separately approved action.

The owner uses the normal Console login and opens `/console/hosted-quota`.
Only their valid human N3 cookie can release. Other administrators, managers,
members, anonymous callers and **all Agent bearer tokens** receive HTTP 403.
The write requires the configured same Origin, a bounded JSON body containing
`hold_id` and `confirm:true`, and rejects unknown fields/trailing JSON. No caller
can supply an actor or switch identities through the request body.

## Release and subsequent start

`POST /api/console/hosted/nodes/{nodeID}/holds/{seatID}/release` authorizes exactly
one current hold generation. The Hub records the verified actor and intent before
sending `release_soft_hold`. It records success/refusal with fixed, sanitized
metadata; rejected identities are audited without request bodies or credentials.

The authenticated node requires soft mode, a live private controller, the exact
persistent hold ID, confirmed stop, fresh known usage/root/resource/inventory
snapshots, existing reserve gates and per-seat charged usage at or below the
configured recovery percentage. Enforcement contention refuses rather than
waiting indefinitely. Retiring login writers cannot be released. No new policy
thresholds or permissive recovery defaults are introduced.

Clearing the hold is persisted before success. Persistence uncertainty latches
the existing fail-closed controller. Fresh hazards get fresh IDs; old approval
and ledger replay cannot clear a later hold. No data, credentials, volumes or
history are removed, and neither monitor nor notification worker clears holds.

Release **never starts/restarts a container or resumes credentials**. A successful
response is `released_stopped`. The owner must refresh the monitor; a subsequent
explicit normal restart still traverses the existing soft and B capacity gates
and waits for the Agent's new online connection. The Hub accepts stopped seats
for restart only after `soft_quota_released` confirmation.

A timeout or lost reply is uncertain: the page disables that submission and does
not resend it. A later authenticated capacity frame can confirm the exact
`last_release_id` against the previously audited owner intent, record an
idempotent `release_confirmed` audit and clear the Hub's held detail while keeping
the seat stopped. No explicit owner intent means no reconciliation release.
An operator must inspect uncertain state, not guess that a failed HTTP request
means the command had no effect.

## Notifications and monitoring

Existing allowlisted warning/hold events atomically create two outbox rows along
with their audit receipt and capacity update. Only after commit can the node
acknowledge/discard an event. `g4_quota_notice` is idempotent by node/event/recipient.

The context-bound worker checks every 15 seconds, processes at most 32 rows with
a 10-second batch budget and 5-second per-delivery budget, and defers failed
routes 30 seconds so they do not starve other notices. Each destination has its
own stable delivery ID and receipt. Failed delivery does not erase the event or
mark the other destination delivered. The owner enters the synchronous durable
Feishu outbound store for their actual bound issuer/subject. Prodops enters the
exact Agent's durable session queue, requiring known delivery-ACK capability.
No in-memory asynchronous enqueue is treated as durable completion.

The message includes fixed warning/hold wording, safe node/seat/event IDs and UTC
measurement time, never raw Engine errors, host paths, CIDs, account data or
credentials. Hold text distinguishes "stopped or stopping" from confirmed stop.
Prodops messages are system notifications with `no_reply`, not a release grant.
`notices_queued` means **durably queued**, not human read or transport completion.
Existing delivery queues manage delivery/ACK; a crash before the outbox receipt
may enqueue the same stable ID again, which the durable queue deduplicates.

`GET /api/console/hosted/quota-monitor` permits the configured human owner or the
exact active prodops Agent's existing bearer credential. Prodops gets
`can_release:false`, and cannot use the owner page or any release endpoint. A
suspended/revoked Agent or inactive owner fails normal Agent authentication.
Consumers must handle credentials in memory through the established private
credential path, never put them in URLs, command lines, logs or evidence.

The projection lists nodes, soft seat/login writer usage (allocated and apparent),
limits, warning/unknown flags, hold ID/reason/stop state, public reserve/emergency
and recovery thresholds, snapshot age and per-node notice counts. It excludes
labels, Engine/socket/private paths, CIDs and arbitrary capacity fields. Expired
node frames are marked stale; filesystem `known` and timestamp are separate from
Hub heartbeat freshness. Hard nodes remain listed as hard without inventing
missing soft-accounting measurements. Responses are `no-store`.

The proposed prodops monitoring cadence is once per existing 30-second usage cycle while a trial
is active, checks age/unknown, both usage measures, warning/recovery percentages,
root bytes/inodes and their ordinary/emergency reserves, hold/stop state and
pending notices, and forwards early warnings/hold failures to the owner. Prodops
does not release holds. This page/API does not itself authorize a live trial or
replace the separate node/network/owner readiness gates.

## Compatibility and verification boundary

Hard quota, CPU/memory/PID/log limits, policy revision, A1/N2/B, helper package and
dependency lock remain unchanged. The additive node command is advertised as
`hold_release_version:1`; old nodes have no such capability and cannot be released
through this endpoint. Existing version-1 private soft state gets an opaque hold
generation persisted for existing holds without clearing or replaying history.
The helper runtime is unchanged, so no helper/package version bump is required.

Schema apply/drop now includes a thirteenth table, `g4_quota_notice`, and indexes.
The outbox has no cascading deletion dependency on destroyed seats/login writers.
Rollback to old application code leaves this additive table and unknown JSON
fields intact and disables the new release command. Do not delete private state,
outbox or audits as a rollback shortcut. Actual Engine, Feishu delivery, target
node state/filesystem and owner/prodops enrollment acceptance require separately
approved real-environment verification; local fakes are not that acceptance.
