CREATE TABLE IF NOT EXISTS console_session (
  id TEXT PRIMARY KEY,
  principal_id TEXT NOT NULL REFERENCES console_principal(id),
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  revoked_at TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS console_page_capability (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES console_session(id),
  principal_id TEXT NOT NULL REFERENCES console_principal(id),
  agent_id TEXT NOT NULL REFERENCES console_agent(id),
  page_id TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  generation INTEGER NOT NULL,
  expires_at TEXT NOT NULL,
  revoked_at TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(session_id, agent_id, page_id, generation)
);

CREATE INDEX IF NOT EXISTS idx_console_session_token ON console_session(token_hash);
CREATE INDEX IF NOT EXISTS idx_console_cap_page ON console_page_capability(session_id, agent_id, page_id, generation);
