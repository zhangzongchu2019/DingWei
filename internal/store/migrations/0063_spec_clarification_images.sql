-- Hub stores question ACL and immutable attachment links, not image bytes/URLs.
-- Private bytes remain in file-service's separate SQLite-only image table.
CREATE TABLE spec_clarification_image (
 file_id TEXT PRIMARY KEY, question_id TEXT NOT NULL REFERENCES pilot_question(id),
 actor_id TEXT NOT NULL, media_type TEXT NOT NULL, sha256 TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes>0 AND size_bytes<=5242880),
 created_at TEXT NOT NULL
);
CREATE INDEX idx_clarification_image_actor ON spec_clarification_image(question_id,actor_id);
CREATE TABLE spec_clarification_post_image (
 spec_id TEXT NOT NULL, floor INTEGER NOT NULL, file_id TEXT NOT NULL UNIQUE REFERENCES spec_clarification_image(file_id),
 PRIMARY KEY(spec_id,floor,file_id), FOREIGN KEY(spec_id,floor) REFERENCES spec_clarification_post(spec_id,floor)
);
CREATE TRIGGER clarification_post_image_no_update BEFORE UPDATE ON spec_clarification_post_image BEGIN SELECT RAISE(ABORT,'clarification image links append only'); END;
CREATE TRIGGER clarification_post_image_no_delete BEFORE DELETE ON spec_clarification_post_image BEGIN SELECT RAISE(ABORT,'clarification image links append only'); END;
CREATE TRIGGER clarification_image_no_update BEFORE UPDATE ON spec_clarification_image BEGIN SELECT RAISE(ABORT,'clarification image metadata immutable'); END;
CREATE TRIGGER clarification_image_no_delete BEFORE DELETE ON spec_clarification_image BEGIN SELECT RAISE(ABORT,'clarification image metadata immutable'); END;

-- Existing owner-selected PMs remain explicit current PMs; no deputy is
-- inferred and historical answers are not relabelled as authoritative.
INSERT INTO spec_clarification_policy(spec_id,pm_user,deputy_user,version,updated_by,updated_at)
 SELECT q.spec_id,q.pm_user,'',1,q.updated_by,q.updated_at FROM pilot_question_policy q
 WHERE NOT EXISTS(SELECT 1 FROM spec_clarification_policy p WHERE p.spec_id=q.spec_id);
INSERT INTO spec_clarification_policy_history(spec_id,version,pm_user,deputy_user,actor_id,created_at)
 SELECT p.spec_id,p.version,p.pm_user,p.deputy_user,p.updated_by,p.updated_at FROM spec_clarification_policy p
 WHERE NOT EXISTS(SELECT 1 FROM spec_clarification_policy_history h WHERE h.spec_id=p.spec_id AND h.version=p.version);
