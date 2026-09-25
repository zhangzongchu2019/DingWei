CREATE TABLE console_spec_gate_review (
  id TEXT PRIMARY KEY,
  spec_id TEXT NOT NULL REFERENCES console_spec(id),
  revision INTEGER NOT NULL,
  review_agent_id TEXT NOT NULL,
  operator_principal TEXT NOT NULL REFERENCES console_principal(id),
  issues_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(spec_id, revision),
  FOREIGN KEY(spec_id, revision) REFERENCES console_spec_revision(spec_id, revision)
);

CREATE INDEX idx_console_spec_gate_review_agent
  ON console_spec_gate_review(review_agent_id, updated_at, spec_id);
