-- R16 step C: configurable aggregate project sources.

CREATE TABLE IF NOT EXISTS project_aggregate_source (
  aggregate_project_id TEXT NOT NULL,
  source_project_id    TEXT NOT NULL,
  created_at           TEXT,
  PRIMARY KEY(aggregate_project_id, source_project_id)
);

CREATE INDEX IF NOT EXISTS idx_project_aggregate_source_source ON project_aggregate_source(source_project_id);
