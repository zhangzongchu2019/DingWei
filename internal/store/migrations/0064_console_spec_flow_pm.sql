ALTER TABLE console_spec_agent_request ADD COLUMN working_version INTEGER NOT NULL DEFAULT 0 CHECK(working_version>=0);
ALTER TABLE console_spec_revision ADD COLUMN source_refs_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE console_spec_revision ADD COLUMN norm_anchors_json TEXT NOT NULL DEFAULT '[]';

-- Mutable autosave state is kept separately from immutable console_spec_revision.
-- A new spec does not enter the immutable revision ledger until an explicit freeze action.
CREATE TABLE console_spec_working_draft (
 spec_id TEXT PRIMARY KEY,
 principal_id TEXT NOT NULL REFERENCES console_principal(id),
 current_editor_id TEXT NOT NULL REFERENCES console_principal(id),
 title TEXT NOT NULL,
 content_markdown TEXT NOT NULL,
 acceptance_json TEXT NOT NULL,
 input_anchors_json TEXT NOT NULL,
 source_refs_json TEXT NOT NULL DEFAULT '[]',
 norm_anchors_json TEXT NOT NULL DEFAULT '[]',
 version INTEGER NOT NULL CHECK(version>0),
 content_sha256 TEXT NOT NULL CHECK(length(content_sha256)=64),
 base_revision INTEGER NOT NULL DEFAULT 0 CHECK(base_revision>=0),
 state TEXT NOT NULL CHECK(state IN ('editing','review','review_complete','final')),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX console_spec_working_draft_editor ON console_spec_working_draft(current_editor_id,state,updated_at);

-- Preserve both submitted and current bytes on CAS conflict; conflict rows are append-only.
CREATE TABLE console_spec_working_draft_conflict (
 id TEXT PRIMARY KEY,
 spec_id TEXT NOT NULL,
 actor_id TEXT NOT NULL,
 expected_version INTEGER NOT NULL,
 expected_sha256 TEXT NOT NULL,
 submitted_json TEXT NOT NULL,
 current_json TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE TRIGGER console_spec_working_draft_conflict_no_update BEFORE UPDATE ON console_spec_working_draft_conflict BEGIN SELECT RAISE(ABORT,'spec draft conflicts append only'); END;
CREATE TRIGGER console_spec_working_draft_conflict_no_delete BEFORE DELETE ON console_spec_working_draft_conflict BEGIN SELECT RAISE(ABORT,'spec draft conflicts append only'); END;

CREATE TABLE console_spec_flow (
 spec_id TEXT PRIMARY KEY,
 original_editor_id TEXT NOT NULL REFERENCES console_principal(id),
 review_recipient_id TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('editing','review','review_complete','final')),
 frozen_revision INTEGER NOT NULL DEFAULT 0 CHECK(frozen_revision>=0),
 frozen_sha256 TEXT NOT NULL DEFAULT '',
 review_request_id TEXT NOT NULL DEFAULT '',
 review_sha256 TEXT NOT NULL DEFAULT '',
 decisions_json TEXT NOT NULL DEFAULT '[]',
 completed_by TEXT NOT NULL DEFAULT '',
 completed_at TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE console_spec_flow_event (
 id TEXT PRIMARY KEY,
 spec_id TEXT NOT NULL,
 actor_id TEXT NOT NULL,
 action TEXT NOT NULL,
 revision INTEGER NOT NULL DEFAULT 0,
 content_sha256 TEXT NOT NULL DEFAULT '',
 detail_json TEXT NOT NULL DEFAULT '{}',
 operation_id TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL
);
CREATE INDEX console_spec_flow_event_history ON console_spec_flow_event(spec_id,created_at,id);
CREATE UNIQUE INDEX console_spec_flow_event_operation ON console_spec_flow_event(operation_id) WHERE operation_id<>'';

-- A PM assist is immutable evidence tied to the precise working draft bytes.
CREATE TABLE console_spec_flow_assist (
 request_id TEXT PRIMARY KEY,
 spec_id TEXT NOT NULL,
 actor_id TEXT NOT NULL,
 agent_id TEXT NOT NULL,
 working_version INTEGER NOT NULL CHECK(working_version>0),
 content_sha256 TEXT NOT NULL CHECK(length(content_sha256)=64),
 status TEXT NOT NULL CHECK(status IN ('pending','completed','stale','failed')),
 result_json TEXT NOT NULL DEFAULT '{}',
 hub_boot TEXT NOT NULL DEFAULT '',
 deadline INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX console_spec_flow_assist_latest ON console_spec_flow_assist(spec_id,actor_id,created_at DESC);
CREATE TABLE console_spec_flow_decision (
 request_id TEXT NOT NULL REFERENCES console_spec_flow_assist(request_id),
 item_id TEXT NOT NULL,
 outcome TEXT NOT NULL CHECK(outcome IN ('accept','reject')),
 reason TEXT NOT NULL DEFAULT '',
 actor_id TEXT NOT NULL,
 created_at TEXT NOT NULL,
 PRIMARY KEY(request_id,item_id)
);
CREATE TRIGGER console_spec_flow_decision_no_update BEFORE UPDATE ON console_spec_flow_decision BEGIN SELECT RAISE(ABORT,'spec flow decisions append only'); END;
CREATE TRIGGER console_spec_flow_decision_no_delete BEFORE DELETE ON console_spec_flow_decision BEGIN SELECT RAISE(ABORT,'spec flow decisions append only'); END;
