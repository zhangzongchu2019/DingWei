CREATE TABLE IF NOT EXISTS console_principal (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  provider_issuer TEXT NOT NULL,
  provider_subject TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
  UNIQUE(provider, provider_issuer, provider_subject)
);

CREATE TABLE IF NOT EXISTS console_agent (
  id TEXT PRIMARY KEY,
  key_id TEXT NOT NULL,
  session_name TEXT NOT NULL,
  owner_principal TEXT NOT NULL REFERENCES console_principal(id),
  active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
  UNIQUE(key_id, session_name)
);

CREATE TABLE IF NOT EXISTS console_agent_grant (
  principal_id TEXT NOT NULL REFERENCES console_principal(id),
  agent_id TEXT NOT NULL REFERENCES console_agent(id),
  permission TEXT NOT NULL CHECK(permission IN ('view','interact')),
  active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
  PRIMARY KEY(principal_id, agent_id, permission)
);

CREATE INDEX IF NOT EXISTS idx_console_agent_owner ON console_agent(owner_principal, active);
CREATE INDEX IF NOT EXISTS idx_console_grant_principal ON console_agent_grant(principal_id, active);
