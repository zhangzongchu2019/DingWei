-- M7 reminder idempotency log.
CREATE TABLE IF NOT EXISTS notification_log (
  id          TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  owner_key   TEXT NOT NULL,
  target_key  TEXT NOT NULL,
  remind_date TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  UNIQUE(kind, owner_key, target_key, remind_date)
);
