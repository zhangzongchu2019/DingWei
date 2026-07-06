-- R5/R6: member candidate discovery + encrypted multi-bot app_secret.
ALTER TABLE bot_channel ADD COLUMN app_secret_enc TEXT;

CREATE TABLE IF NOT EXISTS seen_person (
  open_id        TEXT NOT NULL,
  bot_channel_id TEXT NOT NULL,
  name           TEXT,
  source         TEXT NOT NULL,
  last_seen_at   TEXT,
  PRIMARY KEY(open_id, bot_channel_id)
);
