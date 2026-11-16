CREATE TABLE outbound_intent (
 sender_id TEXT NOT NULL,
 source_message_id TEXT NOT NULL,
 target_id TEXT NOT NULL,
 digest TEXT NOT NULL,
 delivery_id TEXT NOT NULL UNIQUE,
 created_at TEXT NOT NULL,
 PRIMARY KEY(sender_id,source_message_id)
);
-- Intentionally no cascading FK: this small fence outlives delivery payloads.
CREATE INDEX outbound_intent_sender ON outbound_intent(sender_id);
