-- 0067 belongs to the separately frozen R11 branch. Keep that migration intact.
-- No FK cascade from principals/projects: confirmation history is immutable.
CREATE TABLE openspec_commit_confirmation (
 id TEXT PRIMARY KEY CHECK(length(id)=64),
 operation_id TEXT NOT NULL,
 git_instance TEXT NOT NULL,
 repository TEXT NOT NULL,
 change_dir TEXT NOT NULL,
 commit_sha TEXT NOT NULL CHECK(length(commit_sha)=40 AND commit_sha NOT GLOB '*[^0-9a-f]*'),
 confirmed_by TEXT NOT NULL,
 confirmed_at TEXT NOT NULL,
 meaning TEXT NOT NULL CHECK(length(trim(meaning))>0),
 UNIQUE(confirmed_by,operation_id)
);
CREATE INDEX idx_openspec_commit_confirmation_change ON openspec_commit_confirmation(git_instance,repository,change_dir,confirmed_at,id);
CREATE TRIGGER openspec_commit_confirmation_no_update BEFORE UPDATE ON openspec_commit_confirmation
 BEGIN SELECT RAISE(ABORT,'OpenSpec confirmations are immutable'); END;
CREATE TRIGGER openspec_commit_confirmation_no_delete BEFORE DELETE ON openspec_commit_confirmation
 BEGIN SELECT RAISE(ABORT,'OpenSpec confirmations are immutable'); END;
