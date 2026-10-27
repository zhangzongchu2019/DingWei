-- Rebuild only the kind CHECK. Run on the existing dedicated FK-disabled
-- migration connection, with pre/post FK checks and transactional ledger.
-- No body, authority, floor, relationship or attachment is reclassified.
DROP TRIGGER spec_clarification_body_no_update;
DROP TRIGGER spec_clarification_body_no_delete;
CREATE TABLE spec_clarification_post_supplement (
 spec_id TEXT NOT NULL REFERENCES console_spec(id), floor INTEGER NOT NULL CHECK(floor>0),
 revision INTEGER NOT NULL, content_sha256 TEXT NOT NULL,
 question_id TEXT NOT NULL, answer_actor TEXT NOT NULL, answer_event TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN('pm','deputy','technical')),
 kind TEXT NOT NULL CHECK(kind IN('supplement','explanation','scope_change','technical','confirmation','correction')),
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
INSERT INTO spec_clarification_post_supplement SELECT * FROM spec_clarification_post;
DROP TABLE spec_clarification_post;
ALTER TABLE spec_clarification_post_supplement RENAME TO spec_clarification_post;
CREATE INDEX idx_spec_clarification_question ON spec_clarification_post(question_id,floor);
CREATE INDEX idx_spec_clarification_revision ON spec_clarification_post(spec_id,revision,floor);
CREATE INDEX idx_spec_clarification_parent ON spec_clarification_post(parent_key,answer_actor,floor);
CREATE TRIGGER spec_clarification_post_no_update BEFORE UPDATE ON spec_clarification_post
 BEGIN SELECT RAISE(ABORT,'clarification posts are append-only'); END;
CREATE TRIGGER spec_clarification_post_no_delete BEFORE DELETE ON spec_clarification_post
 BEGIN SELECT RAISE(ABORT,'clarification posts are append-only'); END;
CREATE TRIGGER spec_clarification_body_no_update BEFORE UPDATE ON pilot_question_answer
 WHEN EXISTS(SELECT 1 FROM spec_clarification_post p WHERE p.question_id=OLD.question_id AND p.answer_actor=OLD.actor_id AND p.answer_event=OLD.event_id)
 BEGIN SELECT RAISE(ABORT,'clarification bodies are append-only'); END;
CREATE TRIGGER spec_clarification_body_no_delete BEFORE DELETE ON pilot_question_answer
 WHEN EXISTS(SELECT 1 FROM spec_clarification_post p WHERE p.question_id=OLD.question_id AND p.answer_actor=OLD.actor_id AND p.answer_event=OLD.event_id)
 BEGIN SELECT RAISE(ABORT,'clarification bodies are append-only'); END;
