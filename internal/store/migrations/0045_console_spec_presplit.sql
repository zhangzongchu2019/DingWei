CREATE TABLE IF NOT EXISTS console_spec_presplit_request (
  id TEXT PRIMARY KEY,
  spec_id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  agent_id TEXT NOT NULL REFERENCES console_agent(id),
  owner_principal TEXT NOT NULL REFERENCES console_principal(id),
  status TEXT NOT NULL CHECK(status IN ('pending','completed','failed')),
  failure_reason TEXT NOT NULL DEFAULT '',
  deadline_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  completed_at TEXT,
  UNIQUE(spec_id, revision),
  FOREIGN KEY(spec_id, revision) REFERENCES console_spec_revision(spec_id, revision)
);

CREATE INDEX IF NOT EXISTS idx_console_spec_presplit_status_deadline
  ON console_spec_presplit_request(status, deadline_at);
