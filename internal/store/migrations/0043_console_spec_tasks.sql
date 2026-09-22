CREATE TABLE IF NOT EXISTS console_spec_task (
  id TEXT PRIMARY KEY,
  spec_id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  content_sha256 TEXT NOT NULL,
  title TEXT NOT NULL,
  task_content TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'planned',
  estimated_hours REAL NOT NULL DEFAULT 0,
  depends_on_json TEXT NOT NULL DEFAULT '[]',
  acceptance_json TEXT NOT NULL,
  input_anchors_json TEXT NOT NULL,
  created_by TEXT NOT NULL REFERENCES console_principal(id),
  created_at TEXT NOT NULL,
  FOREIGN KEY(spec_id, revision) REFERENCES console_spec_revision(spec_id, revision)
);

CREATE INDEX IF NOT EXISTS idx_console_spec_task_spec ON console_spec_task(spec_id, revision, created_at, id);
