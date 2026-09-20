CREATE TABLE IF NOT EXISTS console_spec (
  id TEXT PRIMARY KEY,
  principal_id TEXT NOT NULL REFERENCES console_principal(id),
  title TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('draft','hard_gate','human_review','needs_improvement','claimable','in_progress','archived')),
  current_revision INTEGER NOT NULL CHECK(current_revision > 0),
  claimed_by TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_console_spec_owner_state
  ON console_spec(principal_id, state, updated_at, id);

CREATE TABLE IF NOT EXISTS console_spec_revision (
  spec_id TEXT NOT NULL REFERENCES console_spec(id),
  revision INTEGER NOT NULL CHECK(revision > 0),
  content_markdown TEXT NOT NULL,
  acceptance_json TEXT NOT NULL,
  input_anchors_json TEXT NOT NULL,
  content_sha256 TEXT NOT NULL,
  created_by TEXT NOT NULL REFERENCES console_principal(id),
  supersedes_revision INTEGER,
  created_at TEXT NOT NULL,
  PRIMARY KEY(spec_id, revision)
);

CREATE TABLE IF NOT EXISTS console_spec_review (
  id TEXT PRIMARY KEY,
  spec_id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  stage TEXT NOT NULL CHECK(stage IN ('hard_gate','human_review')),
  verdict TEXT NOT NULL CHECK(verdict IN ('pass','fail')),
  reason_codes_json TEXT NOT NULL DEFAULT '[]',
  detail TEXT NOT NULL DEFAULT '',
  reviewed_by TEXT NOT NULL REFERENCES console_principal(id),
  reviewed_at TEXT NOT NULL,
  FOREIGN KEY(spec_id, revision) REFERENCES console_spec_revision(spec_id, revision)
);

CREATE INDEX IF NOT EXISTS idx_console_spec_review_gate
  ON console_spec_review(spec_id, revision, stage, verdict, reviewed_at);

CREATE TABLE IF NOT EXISTS console_task_proposal (
  id TEXT PRIMARY KEY,
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  spec_id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  content_sha256 TEXT NOT NULL,
  title TEXT NOT NULL,
  acceptance_json TEXT NOT NULL,
  input_anchors_json TEXT NOT NULL,
  review_id TEXT NOT NULL REFERENCES console_spec_review(id),
  reviewed_by TEXT NOT NULL REFERENCES console_principal(id),
  reviewed_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(spec_id, revision),
  FOREIGN KEY(spec_id, revision) REFERENCES console_spec_revision(spec_id, revision)
);

CREATE TABLE IF NOT EXISTS console_spec_event (
  id TEXT PRIMARY KEY,
  spec_id TEXT NOT NULL REFERENCES console_spec(id),
  revision INTEGER NOT NULL,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_console_spec_event_history
  ON console_spec_event(spec_id, at, id);
