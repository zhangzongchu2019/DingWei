CREATE TABLE IF NOT EXISTS key_application (
  id                 TEXT PRIMARY KEY,
  applicant_open_id  TEXT NOT NULL,
  applicant_account  TEXT NOT NULL,
  applicant_bot_id   TEXT NOT NULL,
  applicant_bot_name TEXT NOT NULL,
  description        TEXT NOT NULL,
  status             TEXT NOT NULL DEFAULT 'pending',
  approver_open_id   TEXT,
  service_id         TEXT,
  key_id             TEXT,
  reject_reason      TEXT,
  created_at         TEXT NOT NULL,
  reviewed_at        TEXT
);
CREATE INDEX IF NOT EXISTS idx_key_application_status_created ON key_application(status, created_at);
