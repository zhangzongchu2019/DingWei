ALTER TABLE console_spec_task ADD COLUMN group_code TEXT NOT NULL DEFAULT '';
ALTER TABLE console_spec_task ADD COLUMN version INTEGER NOT NULL DEFAULT 1 CHECK(version>0);
ALTER TABLE console_spec_task ADD COLUMN assignment_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE console_spec_task ADD COLUMN needs_reassignment INTEGER NOT NULL DEFAULT 0 CHECK(needs_reassignment IN(0,1));
CREATE TABLE console_task_candidate (
 request_id TEXT PRIMARY KEY REFERENCES console_spec_presplit_request(id) ON DELETE CASCADE,
 group_code TEXT NOT NULL DEFAULT '', tasks_json TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1,
 confirmed_at TEXT, confirmed_by TEXT
);
CREATE TABLE console_task_flow_request (
 actor_id TEXT NOT NULL, request_id TEXT NOT NULL, payload_sha TEXT NOT NULL,
 result_json TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(actor_id,request_id)
);
CREATE TABLE console_task_flow_audit (
 id INTEGER PRIMARY KEY, task_id TEXT NOT NULL, actor_id TEXT NOT NULL,
 operation TEXT NOT NULL, version INTEGER NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE console_task_flow_outbox (
 id TEXT PRIMARY KEY, task_id TEXT NOT NULL, assignment_version INTEGER NOT NULL,
 event TEXT NOT NULL, recipient_agent_id TEXT NOT NULL, role TEXT NOT NULL,
 body TEXT NOT NULL, created_at TEXT NOT NULL, dispatched_at TEXT, terminal_reason TEXT NOT NULL DEFAULT '',
 UNIQUE(task_id,assignment_version,event,recipient_agent_id,role)
);
CREATE INDEX idx_console_task_flow_outbox_pending ON console_task_flow_outbox(dispatched_at,created_at,id);
