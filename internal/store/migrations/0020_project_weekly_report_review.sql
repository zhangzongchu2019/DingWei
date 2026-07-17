-- R23: aggregate weekly report review state (spec v0.23 §3.5.10 stage 2B/3).

ALTER TABLE project_weekly_report ADD COLUMN status TEXT NOT NULL DEFAULT 'final';
ALTER TABLE project_weekly_report ADD COLUMN approved_at TEXT;
ALTER TABLE project_weekly_report ADD COLUMN vetoed_at TEXT;
ALTER TABLE project_weekly_report ADD COLUMN published_at TEXT;
ALTER TABLE project_weekly_report ADD COLUMN updated_at TEXT;

UPDATE project_weekly_report SET status='final' WHERE status IS NULL OR status='';
UPDATE project_weekly_report SET updated_at=created_at WHERE updated_at IS NULL OR updated_at='';
