-- Additive thread records. Bodies remain exclusively in pilot_question_answer;
-- old answers retain their historical, unverified authority classification.
CREATE TABLE spec_clarification_policy (
 spec_id TEXT PRIMARY KEY REFERENCES console_spec(id),
 pm_user TEXT NOT NULL, deputy_user TEXT NOT NULL DEFAULT '',
 version INTEGER NOT NULL CHECK(version>0),
 updated_by TEXT NOT NULL, updated_at TEXT NOT NULL,
 CHECK(pm_user='' OR pm_user<>deputy_user)
);
CREATE TABLE spec_clarification_policy_history (
 spec_id TEXT NOT NULL REFERENCES console_spec(id), version INTEGER NOT NULL,
 pm_user TEXT NOT NULL, deputy_user TEXT NOT NULL,
 actor_id TEXT NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(spec_id,version)
);
CREATE TABLE spec_clarification_floor (
 spec_id TEXT PRIMARY KEY REFERENCES console_spec(id), next_floor INTEGER NOT NULL CHECK(next_floor>0)
);
CREATE TABLE spec_clarification_post (
 spec_id TEXT NOT NULL REFERENCES console_spec(id), floor INTEGER NOT NULL CHECK(floor>0),
 revision INTEGER NOT NULL, content_sha256 TEXT NOT NULL,
 question_id TEXT NOT NULL, answer_actor TEXT NOT NULL, answer_event TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN('pm','deputy','technical')),
 kind TEXT NOT NULL CHECK(kind IN('explanation','scope_change','technical','confirmation','correction')),
 entry_refs_json TEXT NOT NULL, corrects_floor INTEGER NOT NULL DEFAULT 0,
 confirms_floor INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT '',
 source TEXT NOT NULL CHECK(source IN('console','agent','feishu')),
 parent_key TEXT NOT NULL DEFAULT '', previous_floor INTEGER NOT NULL DEFAULT 0,
 policy_version INTEGER NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(spec_id,floor),
 UNIQUE(question_id,answer_actor,answer_event),
 FOREIGN KEY(question_id,answer_actor,answer_event) REFERENCES pilot_question_answer(question_id,actor_id,event_id),
 FOREIGN KEY(spec_id,revision) REFERENCES console_spec_revision(spec_id,revision)
);
CREATE INDEX idx_spec_clarification_question ON spec_clarification_post(question_id,floor);
CREATE INDEX idx_spec_clarification_revision ON spec_clarification_post(spec_id,revision,floor);
CREATE INDEX idx_spec_clarification_parent ON spec_clarification_post(parent_key,answer_actor,floor);
CREATE TABLE spec_clarification_request (
 actor_id TEXT NOT NULL, request_id TEXT NOT NULL, payload_sha TEXT NOT NULL,
 spec_id TEXT NOT NULL, floor INTEGER NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(actor_id,request_id),
 FOREIGN KEY(spec_id,floor) REFERENCES spec_clarification_post(spec_id,floor)
);
CREATE TABLE spec_clarification_anchor (
 spec_id TEXT NOT NULL, revision INTEGER NOT NULL, content_sha256 TEXT NOT NULL,
 anchor_id TEXT NOT NULL, line_number INTEGER NOT NULL CHECK(line_number>0),
 PRIMARY KEY(spec_id,revision,anchor_id),
 FOREIGN KEY(spec_id,revision) REFERENCES console_spec_revision(spec_id,revision)
);
-- Immutable metadata and policy history never silently rewrite old authority.
CREATE TRIGGER spec_clarification_post_no_update BEFORE UPDATE ON spec_clarification_post
 BEGIN SELECT RAISE(ABORT,'clarification posts are append-only'); END;
CREATE TRIGGER spec_clarification_post_no_delete BEFORE DELETE ON spec_clarification_post
 BEGIN SELECT RAISE(ABORT,'clarification posts are append-only'); END;
CREATE TRIGGER spec_clarification_history_no_update BEFORE UPDATE ON spec_clarification_policy_history
 BEGIN SELECT RAISE(ABORT,'clarification policy history is append-only'); END;
CREATE TRIGGER spec_clarification_history_no_delete BEFORE DELETE ON spec_clarification_policy_history
 BEGIN SELECT RAISE(ABORT,'clarification policy history is append-only'); END;
CREATE TRIGGER spec_clarification_body_no_update BEFORE UPDATE ON pilot_question_answer
 WHEN EXISTS(SELECT 1 FROM spec_clarification_post p WHERE p.question_id=OLD.question_id AND p.answer_actor=OLD.actor_id AND p.answer_event=OLD.event_id)
 BEGIN SELECT RAISE(ABORT,'clarification bodies are append-only'); END;
CREATE TRIGGER spec_clarification_body_no_delete BEFORE DELETE ON pilot_question_answer
 WHEN EXISTS(SELECT 1 FROM spec_clarification_post p WHERE p.question_id=OLD.question_id AND p.answer_actor=OLD.actor_id AND p.answer_event=OLD.event_id)
 BEGIN SELECT RAISE(ABORT,'clarification bodies are append-only'); END;
