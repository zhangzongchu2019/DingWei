CREATE TABLE IF NOT EXISTS session_delivery (
  id TEXT PRIMARY KEY,
  key_id TEXT NOT NULL,
  session_name TEXT NOT NULL,
  envelope_json TEXT NOT NULL,
  source_message_id TEXT,
  source_type TEXT NOT NULL DEFAULT 'feishu_inbound',
  state TEXT NOT NULL DEFAULT 'queued',
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  lease_until TEXT,
  last_error TEXT,
  created_at TEXT NOT NULL,
  delivered_at TEXT,
  processed_at TEXT,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_delivery_target ON session_delivery(key_id, session_name, state, next_attempt_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_session_delivery_source ON session_delivery(source_type, source_message_id, key_id, session_name) WHERE source_message_id IS NOT NULL;
