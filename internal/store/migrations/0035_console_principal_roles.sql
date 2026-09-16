CREATE TABLE IF NOT EXISTS console_principal_role (
  principal_id TEXT NOT NULL REFERENCES console_principal(id),
  role TEXT NOT NULL CHECK(role IN ('owner')),
  owner_key TEXT NOT NULL,
  active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
  PRIMARY KEY(principal_id, role, owner_key)
);

CREATE INDEX IF NOT EXISTS idx_console_principal_role_scope
  ON console_principal_role(principal_id, role, active, owner_key);
