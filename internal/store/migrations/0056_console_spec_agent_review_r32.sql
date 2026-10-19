CREATE TABLE console_spec_agent_request (
 id TEXT PRIMARY KEY, spec_id TEXT NOT NULL REFERENCES console_spec(id), revision INTEGER NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('review','criteria')), actor TEXT NOT NULL, agent_id TEXT NOT NULL,
 key_id TEXT NOT NULL, session_name TEXT NOT NULL, snapshot_sha TEXT NOT NULL, client_id TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','awaiting_confirmation','completed','cancelled','failed','timed_out')),
 deadline INTEGER NOT NULL, boot TEXT NOT NULL, request_json TEXT NOT NULL, result_json TEXT NOT NULL DEFAULT '',
 result_digest TEXT NOT NULL DEFAULT '', failure TEXT NOT NULL DEFAULT '', result_revision INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(actor,client_id)
);
CREATE UNIQUE INDEX console_spec_agent_active ON console_spec_agent_request(spec_id) WHERE status IN ('pending','awaiting_confirmation');
CREATE INDEX console_spec_agent_actor ON console_spec_agent_request(agent_id,id);
CREATE TABLE console_spec_agent_gate (
 spec_id TEXT NOT NULL, revision INTEGER NOT NULL, request_id TEXT NOT NULL REFERENCES console_spec_agent_request(id),
 agent_id TEXT NOT NULL, issues_json TEXT NOT NULL, human_confirmed INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(spec_id,revision)
);
CREATE TABLE console_spec_agent_fresh (
 spec_id TEXT NOT NULL, revision INTEGER NOT NULL, PRIMARY KEY(spec_id,revision)
);
CREATE TABLE console_spec_agent_outbox (
 request_id TEXT PRIMARY KEY REFERENCES console_spec_agent_request(id), sent INTEGER NOT NULL DEFAULT 0
);
-- These guards cover old HTTP, capability, Agent and direct store write paths.
-- Completion/cancellation consumes the lease before its writes, in the SAME transaction.
CREATE TRIGGER console_spec_agent_lock_spec BEFORE UPDATE ON console_spec
 WHEN EXISTS(SELECT 1 FROM console_spec_agent_request q WHERE q.spec_id=OLD.id AND q.status IN ('pending','awaiting_confirmation') AND q.deadline>CAST(strftime('%s','now') AS INTEGER))
 BEGIN SELECT RAISE(ABORT,'console spec agent request locked'); END;
CREATE TRIGGER console_spec_agent_lock_delete BEFORE DELETE ON console_spec
 WHEN EXISTS(SELECT 1 FROM console_spec_agent_request q WHERE q.spec_id=OLD.id AND q.status IN ('pending','awaiting_confirmation') AND q.deadline>CAST(strftime('%s','now') AS INTEGER))
 BEGIN SELECT RAISE(ABORT,'console spec agent request locked'); END;
CREATE TRIGGER console_spec_agent_lock_revision BEFORE INSERT ON console_spec_revision
 WHEN EXISTS(SELECT 1 FROM console_spec_agent_request q WHERE q.spec_id=NEW.spec_id AND q.status IN ('pending','awaiting_confirmation') AND q.deadline>CAST(strftime('%s','now') AS INTEGER))
 BEGIN SELECT RAISE(ABORT,'console spec agent request locked'); END;
CREATE TRIGGER console_spec_agent_lock_gate_insert BEFORE INSERT ON console_spec_gate_review
 WHEN EXISTS(SELECT 1 FROM console_spec_agent_request q WHERE q.spec_id=NEW.spec_id AND q.status IN ('pending','awaiting_confirmation') AND q.deadline>CAST(strftime('%s','now') AS INTEGER))
 BEGIN SELECT RAISE(ABORT,'console spec agent request locked'); END;
CREATE TRIGGER console_spec_agent_lock_gate_update BEFORE UPDATE ON console_spec_gate_review
 WHEN EXISTS(SELECT 1 FROM console_spec_agent_request q WHERE q.spec_id=NEW.spec_id AND q.status IN ('pending','awaiting_confirmation') AND q.deadline>CAST(strftime('%s','now') AS INTEGER))
 BEGIN SELECT RAISE(ABORT,'console spec agent request locked'); END;
CREATE TRIGGER console_spec_agent_fresh_finalize BEFORE UPDATE OF state ON console_spec
 WHEN NEW.state='final' AND (EXISTS(SELECT 1 FROM console_spec_agent_fresh f WHERE f.spec_id=NEW.id AND f.revision=NEW.current_revision) OR EXISTS(SELECT 1 FROM console_spec_agent_gate g WHERE g.spec_id=NEW.id AND g.revision=NEW.current_revision))
 AND NOT EXISTS(SELECT 1 FROM console_spec_agent_gate g JOIN console_spec_agent_request q ON q.id=g.request_id WHERE g.spec_id=NEW.id AND g.revision=NEW.current_revision AND g.human_confirmed=1 AND q.status='completed' AND q.id=(SELECT latest.id FROM console_spec_agent_request latest WHERE latest.spec_id=NEW.id AND latest.revision=NEW.current_revision AND latest.kind='review' ORDER BY latest.created_at DESC,latest.id DESC LIMIT 1))
 BEGIN SELECT RAISE(ABORT,'fresh Agent review and human confirmation required'); END;
CREATE TRIGGER console_spec_agent_fresh_descendant AFTER INSERT ON console_spec_revision
 WHEN EXISTS(SELECT 1 FROM console_spec_agent_fresh f WHERE f.spec_id=NEW.spec_id AND f.revision=NEW.supersedes_revision)
 BEGIN INSERT OR IGNORE INTO console_spec_agent_fresh(spec_id,revision) VALUES(NEW.spec_id,NEW.revision); END;
