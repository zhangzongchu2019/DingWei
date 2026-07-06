-- R22: persisted per-project weekly reports (spec v0.23 §3.5.10 stage 2A).

CREATE TABLE IF NOT EXISTS project_weekly_report (
  id         TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  week       TEXT NOT NULL,
  content    TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(project_id, week)
);
CREATE INDEX IF NOT EXISTS idx_project_weekly_report_project_week ON project_weekly_report(project_id, week);
