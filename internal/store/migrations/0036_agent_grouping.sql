CREATE TABLE IF NOT EXISTS agent_group (
  code TEXT PRIMARY KEY CHECK(length(code)=32),
  leader_key_id TEXT NOT NULL,
  leader_session_name TEXT NOT NULL,
  leader_owner_key TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_group_member (
  code TEXT NOT NULL REFERENCES agent_group(code) ON DELETE CASCADE,
  key_id TEXT NOT NULL,
  session_name TEXT NOT NULL,
  owner_key TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(code,key_id,session_name)
);

CREATE INDEX IF NOT EXISTS idx_agent_group_member_endpoint
  ON agent_group_member(key_id,session_name,code);
