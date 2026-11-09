-- Additive rollback audit; retained after feature-off or binary downgrade.
CREATE TABLE sub_agent_rollback (
 root_agent_id TEXT PRIMARY KEY,
 prepared_at TEXT NOT NULL
);
CREATE TABLE sub_agent_rollback_delivery (
 delivery_id TEXT PRIMARY KEY REFERENCES session_delivery(id),
 root_agent_id TEXT NOT NULL,
 original_key_id TEXT NOT NULL,
 original_session_name TEXT NOT NULL,
 classification TEXT NOT NULL CHECK(classification IN ('not_executed','unknown','terminal'))
);
