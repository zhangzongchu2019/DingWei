CREATE TABLE session_history_generation (
 owner_key TEXT NOT NULL,
 agent_key_id TEXT NOT NULL,
 session_name TEXT NOT NULL,
 generation TEXT NOT NULL,
 next_seq INTEGER NOT NULL,
 oldest_seq INTEGER NOT NULL DEFAULT 0,
 newest_seq INTEGER NOT NULL DEFAULT 0,
 gap_through_seq INTEGER NOT NULL DEFAULT 0,
 retained_bytes INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(owner_key,agent_key_id,session_name),
 UNIQUE(owner_key,agent_key_id,session_name,generation)
);
CREATE TABLE session_history_event (
 owner_key TEXT NOT NULL,
 agent_key_id TEXT NOT NULL,
 session_name TEXT NOT NULL,
 generation TEXT NOT NULL,
 seq INTEGER NOT NULL,
 event_id TEXT NOT NULL UNIQUE,
 received_at_ms INTEGER NOT NULL,
 event_type TEXT NOT NULL,
 payload_json BLOB NOT NULL,
 record_bytes INTEGER NOT NULL,
 PRIMARY KEY(owner_key,agent_key_id,session_name,generation,seq)
);
CREATE INDEX idx_history_owner_time ON session_history_event(owner_key,received_at_ms,agent_key_id,session_name,seq);
CREATE INDEX idx_history_agent_time ON session_history_event(owner_key,agent_key_id,session_name,received_at_ms,seq);
CREATE INDEX idx_history_expiry ON session_history_event(received_at_ms,owner_key,agent_key_id,session_name,seq);
CREATE TABLE session_history_gap (
 gap_id TEXT PRIMARY KEY,
 owner_key TEXT NOT NULL,
 agent_key_id TEXT NOT NULL,
 session_name TEXT NOT NULL,
 generation TEXT NOT NULL,
 after_seq INTEGER NOT NULL,
 before_seq INTEGER,
 started_at_ms INTEGER NOT NULL,
 ended_at_ms INTEGER,
 reason TEXT NOT NULL,
 lost_event_count INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_history_gap_range ON session_history_gap(owner_key,agent_key_id,session_name,generation,after_seq,before_seq);
CREATE TABLE session_history_runtime_state (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 enabled INTEGER NOT NULL,
 changed_at_ms INTEGER NOT NULL,
 disabled_since_ms INTEGER,
 initialized INTEGER NOT NULL DEFAULT 0,
 retained_bytes INTEGER NOT NULL DEFAULT 0
);
INSERT INTO session_history_runtime_state(singleton,enabled,changed_at_ms,disabled_since_ms,initialized,retained_bytes)
VALUES(1,0,0,NULL,0,0);
CREATE TABLE session_history_delete_queue (
 owner_key TEXT NOT NULL,
 agent_key_id TEXT NOT NULL,
 requested_at_ms INTEGER NOT NULL,
 attempts INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(owner_key,agent_key_id)
);
