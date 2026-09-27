CREATE TABLE IF NOT EXISTS console_task_driver_start (
  spec_id             TEXT NOT NULL,
  revision            INTEGER NOT NULL,
  content_sha256      TEXT NOT NULL,
  repository_name     TEXT NOT NULL,
  branch_name         TEXT NOT NULL,
  schedule            TEXT NOT NULL,
  task_endpoint       TEXT NOT NULL,
  token_instructions  TEXT NOT NULL,
  announced_by        TEXT NOT NULL REFERENCES console_principal(id),
  announced_at        TEXT NOT NULL,
  owner_reviewed_at   TEXT NULL,
  deployment_requested_at TEXT NULL,
  PRIMARY KEY (spec_id, revision),
  FOREIGN KEY(spec_id, revision) REFERENCES console_spec_revision(spec_id, revision)
);

CREATE TABLE IF NOT EXISTS console_task_pause_report (
  spec_id        TEXT NOT NULL,
  revision       INTEGER NOT NULL,
  agent_id       TEXT NOT NULL REFERENCES console_agent(id),
  reported_at    TEXT NOT NULL,
  PRIMARY KEY (spec_id, revision, agent_id),
  FOREIGN KEY(spec_id, revision) REFERENCES console_spec_revision(spec_id, revision)
);
