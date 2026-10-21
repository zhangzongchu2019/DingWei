-- Separate from the already published 0061 storage checkpoint: no checksum
-- rewriting for databases that ran that checkpoint in an isolated test.
ALTER TABLE message ADD COLUMN feishu_reply_json TEXT NOT NULL DEFAULT '';
-- Every authenticated native reply delivery is checked BEFORE the generic
-- message-ID dedup, including alternate event IDs for an already done message.
CREATE TABLE spec_clarification_transport_key (
 issuer TEXT NOT NULL, tenant TEXT NOT NULL, key_kind TEXT NOT NULL CHECK(key_kind IN('event','message')),
 provider_id TEXT NOT NULL, message_id TEXT NOT NULL, payload_sha TEXT NOT NULL,
 PRIMARY KEY(issuer,tenant,key_kind,provider_id)
);
CREATE TABLE spec_clarification_notice (
 notice_id TEXT PRIMARY KEY REFERENCES pilot_question_outbox(id),
 question_id TEXT NOT NULL REFERENCES pilot_question(id), generation INTEGER NOT NULL,
 spec_id TEXT NOT NULL, revision INTEGER NOT NULL, content_sha256 TEXT NOT NULL,
 pm_user TEXT NOT NULL, policy_version INTEGER NOT NULL,
 issuer TEXT NOT NULL, tenant TEXT NOT NULL, app_id TEXT NOT NULL,
 subject TEXT NOT NULL, binding_sha TEXT NOT NULL,
 entry_refs_json TEXT NOT NULL, provider_message_id TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL, expires_at TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN('prepared','sent','unknown','cancelled'))
);
CREATE UNIQUE INDEX idx_clarification_notice_parent ON spec_clarification_notice(issuer,tenant,provider_message_id) WHERE provider_message_id<>'';
CREATE INDEX idx_clarification_notice_question ON spec_clarification_notice(question_id,generation);
CREATE TABLE spec_clarification_inbound (
 issuer TEXT NOT NULL, tenant TEXT NOT NULL, message_id TEXT NOT NULL,
 event_id TEXT NOT NULL, payload_sha TEXT NOT NULL, metadata_json TEXT NOT NULL,
 body TEXT NOT NULL, received_at TEXT NOT NULL, expires_at TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN('pending_match','recorded','rejected')),
 reason TEXT NOT NULL DEFAULT '', spec_id TEXT NOT NULL DEFAULT '', floor INTEGER NOT NULL DEFAULT 0,
 receipt_state TEXT NOT NULL DEFAULT 'pending' CHECK(receipt_state IN('pending','attempted','sent','unknown')),
 PRIMARY KEY(issuer,tenant,message_id)
);
CREATE TABLE spec_clarification_event_key (
 issuer TEXT NOT NULL, tenant TEXT NOT NULL, event_id TEXT NOT NULL,
 message_id TEXT NOT NULL, payload_sha TEXT NOT NULL,
 PRIMARY KEY(issuer,tenant,event_id)
);
CREATE INDEX idx_clarification_inbound_pending ON spec_clarification_inbound(state,received_at,issuer,tenant,message_id);
CREATE TABLE spec_clarification_question (
 question_id TEXT PRIMARY KEY REFERENCES pilot_question(id),
 entry_refs_json TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE spec_clarification_login_state (
 state_sha TEXT PRIMARY KEY, cookie_sha TEXT NOT NULL,
 question_id TEXT NOT NULL, expires_at TEXT NOT NULL, consumed_at TEXT
);
