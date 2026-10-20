-- Explicit per-spec owner configuration; missing bindings never guess a PM.
CREATE TABLE pilot_question_policy (
 spec_id TEXT PRIMARY KEY REFERENCES console_spec(id),
 pm_user TEXT NOT NULL DEFAULT '', leader_user TEXT NOT NULL DEFAULT '',
 version INTEGER NOT NULL DEFAULT 1, updated_by TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE pilot_question (
 id TEXT PRIMARY KEY, spec_id TEXT NOT NULL REFERENCES console_spec(id),
 revision INTEGER NOT NULL, content_sha256 TEXT NOT NULL,
 task_id TEXT NOT NULL DEFAULT '', blocker_id TEXT NOT NULL DEFAULT '',
 assignment_version INTEGER NOT NULL DEFAULT 0,
 generation INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 1,
 kind TEXT NOT NULL CHECK(kind IN('requirement_unclear','change','technical','resource')),
 title TEXT NOT NULL, observed_fact TEXT NOT NULL, question_text TEXT NOT NULL,
 expected_answer TEXT NOT NULL, input_anchors TEXT NOT NULL DEFAULT '',
 created_by TEXT NOT NULL, pm_user TEXT NOT NULL DEFAULT '',
 responsible_principal TEXT NOT NULL DEFAULT '', leader_user TEXT NOT NULL DEFAULT '',
 mode TEXT NOT NULL DEFAULT 'light' CHECK(mode IN('light','full')),
 state TEXT NOT NULL CHECK(state IN('open','needs_owner','awaiting_answer','answered_pending_recheck','resolved','cancelled','superseded')),
 created_at TEXT NOT NULL, generation_started_at TEXT NOT NULL, due_at TEXT NOT NULL,
 first_substantive_answer_at TEXT, answered_at TEXT, resolved_at TEXT,
 policy_version INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_pilot_question_spec ON pilot_question(spec_id,revision,id);
CREATE INDEX idx_pilot_question_task ON pilot_question(task_id,id);
CREATE INDEX idx_pilot_question_pm ON pilot_question(pm_user,id);
CREATE INDEX idx_pilot_question_due ON pilot_question(first_substantive_answer_at,due_at,id);
CREATE TABLE pilot_question_answer (
 question_id TEXT NOT NULL REFERENCES pilot_question(id), generation INTEGER NOT NULL,
 actor_id TEXT NOT NULL, event_id TEXT NOT NULL, payload_sha TEXT NOT NULL,
 answer_kind TEXT NOT NULL CHECK(answer_kind IN('ack','substantive','technical_input')),
 body TEXT NOT NULL, evidence_ref TEXT NOT NULL, scope_change INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL, PRIMARY KEY(question_id,actor_id,event_id)
);
CREATE UNIQUE INDEX idx_pilot_question_answer_content ON pilot_question_answer(question_id,generation,actor_id,payload_sha);
CREATE TABLE pilot_question_event (
 question_id TEXT NOT NULL REFERENCES pilot_question(id),actor_id TEXT NOT NULL,event_id TEXT NOT NULL,
 generation INTEGER NOT NULL,payload_sha TEXT NOT NULL,canonical_event_id TEXT NOT NULL,
 PRIMARY KEY(question_id,actor_id,event_id)
);
CREATE TABLE pilot_question_request (
 actor_id TEXT NOT NULL, request_id TEXT NOT NULL, payload_sha TEXT NOT NULL,
 question_id TEXT NOT NULL, result_json TEXT NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(actor_id,request_id)
);
CREATE TABLE pilot_question_outbox (
 id TEXT PRIMARY KEY, question_id TEXT NOT NULL REFERENCES pilot_question(id),
 generation INTEGER NOT NULL, kind TEXT NOT NULL CHECK(kind IN('question','assignment','remind_60m')),
 recipient_user TEXT NOT NULL, policy_version INTEGER NOT NULL, event_version INTEGER NOT NULL DEFAULT 0,
 state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN('queued','pending_config','dispatching','sent','unknown','cancelled')),
 created_at TEXT NOT NULL, attempted_at TEXT, finished_at TEXT,
 provider_message_id TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '',
 UNIQUE(question_id,generation,kind,recipient_user,event_version)
);
CREATE INDEX idx_pilot_question_outbox ON pilot_question_outbox(state,id);
CREATE UNIQUE INDEX idx_pilot_question_reminder_once ON pilot_question_outbox(question_id,generation) WHERE kind='remind_60m';
CREATE TABLE pilot_question_change (
 id TEXT PRIMARY KEY, question_id TEXT NOT NULL REFERENCES pilot_question(id),
 generation INTEGER NOT NULL, answer_actor TEXT NOT NULL, answer_event TEXT NOT NULL,
 spec_id TEXT NOT NULL, revision INTEGER NOT NULL, content_sha256 TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN('pending','rejected','confirmed_new_revision')),
 created_at TEXT NOT NULL, decided_by TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '',
 new_revision INTEGER NOT NULL DEFAULT 0, new_content_sha256 TEXT NOT NULL DEFAULT '',
 UNIQUE(question_id,answer_actor,answer_event)
);
CREATE INDEX idx_pilot_question_change_scope ON pilot_question_change(spec_id,revision,state);
CREATE TABLE pilot_question_audit (
 id INTEGER PRIMARY KEY, question_id TEXT NOT NULL, actor_id TEXT NOT NULL,
 operation TEXT NOT NULL, version INTEGER NOT NULL, details_json TEXT NOT NULL, created_at TEXT NOT NULL
);
