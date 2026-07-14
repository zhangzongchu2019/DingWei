CREATE TABLE IF NOT EXISTS session_delivery_attempt (
  id TEXT PRIMARY KEY,
  delivery_id TEXT NOT NULL DEFAULT '',
  key_id TEXT NOT NULL,
  session_name TEXT NOT NULL,
  source_message_id TEXT NOT NULL DEFAULT '',
  source_type TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_delivery_attempt_target ON session_delivery_attempt(key_id, session_name, created_at);
CREATE INDEX IF NOT EXISTS idx_session_delivery_attempt_source ON session_delivery_attempt(source_type, source_message_id, created_at);
