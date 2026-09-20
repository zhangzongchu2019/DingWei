CREATE TABLE IF NOT EXISTS console_platform_item (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  title TEXT NOT NULL,
  href TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('pending','approved')),
  submitted_by TEXT NOT NULL REFERENCES console_principal(id),
  active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_console_platform_item_status
  ON console_platform_item(active, status, updated_at, id);
