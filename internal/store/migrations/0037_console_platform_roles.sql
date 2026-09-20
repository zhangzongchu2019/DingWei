CREATE TABLE IF NOT EXISTS console_platform_role (
  principal_id TEXT NOT NULL REFERENCES console_principal(id),
  role TEXT NOT NULL CHECK(role IN ('management','owner')),
  active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
  PRIMARY KEY(principal_id, role)
);

CREATE INDEX IF NOT EXISTS idx_console_platform_role_active
  ON console_platform_role(principal_id, active, role);
