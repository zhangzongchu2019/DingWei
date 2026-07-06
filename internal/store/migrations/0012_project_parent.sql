-- R16: generic project hierarchy for department/project/aggregate notification inheritance.

ALTER TABLE project ADD COLUMN parent_id TEXT NOT NULL DEFAULT 'proj:default';
CREATE INDEX IF NOT EXISTS idx_project_parent ON project(parent_id);
