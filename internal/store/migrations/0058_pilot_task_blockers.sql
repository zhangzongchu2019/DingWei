-- Opt-in is per task: no existing project/task is silently enrolled.
CREATE TABLE console_task_blocker (
 id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL REFERENCES console_spec_task(id),
 spec_id TEXT NOT NULL, revision INTEGER NOT NULL, content_sha256 TEXT NOT NULL,
 assignment_version INTEGER NOT NULL,
 developer_agent_id TEXT NOT NULL, reviewer_agent_id TEXT NOT NULL,
 generation INTEGER NOT NULL DEFAULT 1 CHECK(generation>0),
 version INTEGER NOT NULL DEFAULT 1 CHECK(version>0),
 kind TEXT NOT NULL,
 where_text TEXT NOT NULL, waiting_principal TEXT NOT NULL DEFAULT '',
 expected_input TEXT NOT NULL, conditions_json TEXT NOT NULL,
 mode TEXT NOT NULL DEFAULT 'light' CHECK(mode IN('light','full')),
 full_approved_by TEXT NOT NULL DEFAULT '', full_approval_ref TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN('waiting','needs_owner','ready_to_recheck','rechecking','resolved','cancelled','needs_reassignment')),
 input_version INTEGER NOT NULL DEFAULT 0, checked_input_version INTEGER NOT NULL DEFAULT 0,
 lease_id TEXT NOT NULL DEFAULT '', lease_until TEXT NOT NULL DEFAULT '',
 created_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 fingerprint TEXT NOT NULL
);
CREATE INDEX idx_console_task_blocker_task ON console_task_blocker(task_id,id);
CREATE TABLE console_task_blocker_input (
 blocker_id TEXT NOT NULL REFERENCES console_task_blocker(id),
 source TEXT NOT NULL, event_id TEXT NOT NULL, payload_sha TEXT NOT NULL,
 input_version INTEGER NOT NULL, actor_id TEXT NOT NULL, body TEXT NOT NULL,
 evidence_ref TEXT NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(blocker_id,source,event_id), UNIQUE(blocker_id,input_version)
);
CREATE TABLE console_task_blocker_audit (
 id INTEGER PRIMARY KEY, blocker_id TEXT NOT NULL, task_id TEXT NOT NULL,
 actor_id TEXT NOT NULL, operation TEXT NOT NULL, version INTEGER NOT NULL,
 details_json TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE console_task_blocker_request (
 actor_id TEXT NOT NULL, request_id TEXT NOT NULL, task_id TEXT NOT NULL,
 payload_sha TEXT NOT NULL, result_json TEXT NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(actor_id,request_id)
);
CREATE TABLE console_task_blocker_outbox (
 id TEXT PRIMARY KEY, blocker_id TEXT NOT NULL REFERENCES console_task_blocker(id),
 task_id TEXT NOT NULL, generation INTEGER NOT NULL, input_version INTEGER NOT NULL,
 assignment_version INTEGER NOT NULL, recipient_agent_id TEXT NOT NULL,
 created_at TEXT NOT NULL, dispatched_at TEXT, terminal_reason TEXT NOT NULL DEFAULT '',
 UNIQUE(blocker_id,generation,input_version,assignment_version,recipient_agent_id)
);
CREATE INDEX idx_console_task_blocker_outbox_pending ON console_task_blocker_outbox(dispatched_at,id);
CREATE TABLE console_task_blocker_condition (
 blocker_id TEXT NOT NULL REFERENCES console_task_blocker(id),
 adapter TEXT NOT NULL, last_sha TEXT NOT NULL, last_satisfied INTEGER NOT NULL,
 event_sequence INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(blocker_id,adapter)
);
