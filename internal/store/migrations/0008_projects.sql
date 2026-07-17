-- R14: project-scoped, versioned schedule documents.

CREATE TABLE IF NOT EXISTS project (
  id              TEXT PRIMARY KEY,
  name            TEXT NOT NULL,
  notify_chat_id  TEXT NOT NULL DEFAULT '',
  notify_bot_id   TEXT NOT NULL DEFAULT 'examplebot',
  transcript_dirs TEXT NOT NULL DEFAULT '',
  evidence_cron   TEXT NOT NULL DEFAULT '',
  evidence_tz     TEXT NOT NULL DEFAULT '',
  active          INTEGER NOT NULL DEFAULT 1,
  created_at      TEXT,
  updated_at      TEXT
);

CREATE TABLE IF NOT EXISTS schedule_doc (
  id          TEXT PRIMARY KEY,
  project_id  TEXT NOT NULL,
  kind        TEXT NOT NULL,
  owner_key   TEXT NOT NULL DEFAULT '',
  version     INTEGER NOT NULL,
  content     TEXT NOT NULL,
  source      TEXT NOT NULL,
  created_by  TEXT,
  created_at  TEXT NOT NULL,
  UNIQUE(project_id, kind, owner_key, version)
);
CREATE INDEX IF NOT EXISTS idx_sched_doc ON schedule_doc(project_id, kind, owner_key, version);

CREATE TABLE IF NOT EXISTS project_member (
  project_id TEXT NOT NULL,
  owner_key  TEXT NOT NULL,
  PRIMARY KEY(project_id, owner_key)
);

CREATE TABLE IF NOT EXISTS seen_person_group (
  open_id        TEXT NOT NULL,
  bot_channel_id TEXT NOT NULL,
  group_chat_id  TEXT NOT NULL,
  group_name     TEXT,
  last_seen_at   TEXT,
  PRIMARY KEY(open_id, bot_channel_id, group_chat_id)
);
