# S-FAILURE-NOTICE-OFFLINE frozen contract

Owner: opus50tc2k. Implementation: coder56solzzc. Independent review: opus50ts.
Base: integration/spec5ff-r9, 7651602335267ef2f8b9957d795e0cfddbd9c08f.
This records the leader assignment when the original single-page design was not locally delivered. It is not deployment authorization.

## R1 — durable terminal transition and notice

The original delivery becoming dead and its eligible sender notice being inserted commit in one SQLite transaction. A failing notice INSERT rolls back the terminal transition. A notice ID is a SHA256 derivation of the failed delivery ID with a fixed versioned domain separator. Retries/restarts/backfill use that same ID. The existing source uniqueness index must distinguish different failed deliveries sharing an original message: notice source_message_id is the failed delivery ID, with failed_message_id preserving the original message reference in metadata.

Reuse session_delivery and the existing ACK/inbox protocol; do not introduce a parallel transport or schema. Notice rows carry Hub-created repository provenance and a distinct source_type. Notice expiry cannot generate another notice. Startup and periodic global sweeps cover every expired nonterminal state, including capability_pending, without requiring either endpoint to reconnect. processed, dead and cancelled remain terminal. Existing 30-minute/attempt limits remain. Startup is bounded by the existing five-second context; periodic cadence remains one minute. A sweep handles up to 1000 expiry rows and 1000 eligible historical rows per pass.

Only previously ACK-capable session sources get the durable bounded-delivery promise. Capability is read from session_endpoint inside the transaction. Unknown/unsupported sources, channel sources, malformed originals and notification rows are ineligible; no durable-delivery promise is inferred from a socket write. Live sender draining accelerates the normal queue; offline senders wait for ordinary authenticated reconnect. The guarantee is at-most-once injection effect through the persistent helper inbox, not exactly-once socket transmission: an ACK loss retries the same ID.

## R2 — trusted routing boundary (slice two)

Before either Agent or channel AMR parsing, derive the current inbound context from the repository using the authenticated connection identity, delivery ID, Hub-created provenance and notice source type. A notice-triggered turn stops both AMR and reply-origin; it must not generate another failure notice. Client-supplied metadata cannot assert this trusted context and is stripped at ingress/egress. Hub-generated kind=reply, no_reply and terminal/suppress_reply_origin semantics provide rollback-era Hub/helper defense in depth. Do not depend solely on an ID prefix, body text or client flag for authority.

## R3 — old helper compatibility (slice two)

These notices are intentionally delivered to the CLI, including rollback-era helpers: use the ordinary ACK delivery envelope, not a system/control type that old helpers discard. First body line is 【系统投递失败通知】. Body identifies only the persisted destination session name (at most64 letters/digits/dot/underscore/hyphen and192 UTF-8 bytes; invalid names use a fixed generic target), plus a fixed allowlisted Chinese reason. It never copies a raw envelope address/account key, original text, arbitrary last_error, credentials, half-width at or hash characters. Client-visible role is a terminal reply with no reply expected. Compatibility and rollback tests must exercise both intended injection and routing suppression.

## R4 — bounded scope and historical recovery

Reason allowlist: retry_exhausted, delivery_ack_unsupported, delivery_capability_unconfirmed_expired, delivery_expired. New retry exhaustion persists a notice atomically while retaining the original LastError diagnostic. Latest leader policy, confirmed by independent review: historical backfill permits only delivery_ack_unsupported, delivery_capability_unconfirmed_expired and delivery_expired, with updated_at within the preceding30 minutes of actual terminal transition, not original created_at. ALL historical retry_exhausted rows are excluded even when explicitly stored; future terminal timestamps do not qualify. No older dead or other terminal row is changed.

Known historical representation gap reported to leader/reviewer: old retry exhaustion stores the helper detail (e.g. cli_unavailable) as LastError; retry_exhausted was only in a transient notice. Arbitrary old detail is not safely classifiable as one of the four reasons. This implementation excludes all historical retry_exhausted rows by policy rather than attempting classification. New retry exhaustion is fully covered. No current deployment data are sampled or invented.

## Delivery slices and verification

Slice one: storage atomicity, deterministic IDs, all-state global cleanup, bounded explicit historical recovery, notification non-recursion, startup/periodic Hub wiring and live drain acceleration. Legacy repository implementations keep their narrow cleanup fallback; the SQLite production candidate implements the global extension. Legacy best-effort source notices outside the known-ACK durability scope are retained until the compatibility/routing slice is reviewed.

Slice two: trusted AMR/reply boundary, metadata stripping, helper dedup/injection and rollback tests. Slice one alone is not a complete release candidate.

Required tests include notice-insert rollback, concurrent sweep dedup, original message shared across targets, offline startup and periodic expiry, processed/dead/cancelled preservation, three permitted historical reasons plus explicit historical retry_exhausted and31-minute cutoff negatives; all four new-transition Chinese reason meanings and sanitized target/leakage negatives, no recursive expiry, ACK-loss/same-ID once-only inbox effect, Agent/channel parser rejection, forged client metadata and rollback-era behavior. No production action is authorized by a source or local test PASS.
