CREATE TABLE session_history_provenance (
 owner_key TEXT NOT NULL,
 agent_key_id TEXT NOT NULL,
 session_name TEXT NOT NULL,
 generation TEXT NOT NULL,
 delivery_id TEXT NOT NULL,
 source_message_id TEXT NOT NULL,
 source_kind TEXT NOT NULL,
 display_name TEXT NOT NULL,
 trust_decision TEXT NOT NULL,
 attested_by TEXT NOT NULL,
 observed_at_ms INTEGER NOT NULL,
 expires_at_ms INTEGER NOT NULL,
 record_bytes INTEGER NOT NULL CHECK(record_bytes>0 AND record_bytes<=4096),
 conflicted INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(owner_key,agent_key_id,session_name,generation,delivery_id)
);
CREATE INDEX idx_history_provenance_expiry ON session_history_provenance(expires_at_ms,owner_key,agent_key_id,session_name);
CREATE INDEX idx_history_provenance_age ON session_history_provenance(observed_at_ms,owner_key,agent_key_id,session_name);
CREATE TRIGGER history_provenance_account_insert AFTER INSERT ON session_history_provenance BEGIN
 UPDATE session_history_generation SET retained_bytes=retained_bytes+NEW.record_bytes WHERE owner_key=NEW.owner_key AND agent_key_id=NEW.agent_key_id AND session_name=NEW.session_name;
 UPDATE session_history_runtime_state SET retained_bytes=retained_bytes+NEW.record_bytes WHERE singleton=1;
END;
CREATE TRIGGER history_provenance_account_delete AFTER DELETE ON session_history_provenance BEGIN
 UPDATE session_history_generation SET retained_bytes=retained_bytes-OLD.record_bytes WHERE owner_key=OLD.owner_key AND agent_key_id=OLD.agent_key_id AND session_name=OLD.session_name;
 UPDATE session_history_runtime_state SET retained_bytes=retained_bytes-OLD.record_bytes WHERE singleton=1;
END;
CREATE TRIGGER history_provenance_generation_change BEFORE UPDATE OF generation ON session_history_generation WHEN OLD.generation<>NEW.generation BEGIN
 DELETE FROM session_history_provenance WHERE owner_key=OLD.owner_key AND agent_key_id=OLD.agent_key_id AND session_name=OLD.session_name;
END;
CREATE TRIGGER history_provenance_generation_delete BEFORE DELETE ON session_history_generation BEGIN
 DELETE FROM session_history_provenance WHERE owner_key=OLD.owner_key AND agent_key_id=OLD.agent_key_id AND session_name=OLD.session_name;
END;
