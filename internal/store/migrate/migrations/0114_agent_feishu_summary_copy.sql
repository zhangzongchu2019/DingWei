-- Per-Agent preference and a generic durable outbox for internal Agent summaries.
-- NULL means use the origin default (external on, internal off); explicit false
-- is durable and wins over the environment gate.
ALTER TABLE np_agent ADD COLUMN IF NOT EXISTS feishu_copy_enabled boolean;
ALTER TABLE np_extseat_feishu_copy ADD COLUMN IF NOT EXISTS group_label text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS np_identity_user_provider ON np_identity(user_id_lc,provider);

-- Normalize pending copies created before the retry deadline was separated from
-- message retention, and retire rows that already exhausted the new cap.
UPDATE np_extseat_feishu_copy SET expires_at=created_at+interval '24 hours'
  WHERE state IN ('queued','sending');
UPDATE np_extseat_feishu_copy SET state='dead',lease_until=NULL,last_error='retry_limit_reached'
  WHERE state IN ('queued','sending') AND attempts>=20;

CREATE TABLE np_agent_feishu_copy_outbox (
  event_id text PRIMARY KEY,
  agent_id text NOT NULL REFERENCES np_agent(id),
  recipient_issuer text NOT NULL,
  recipient_subject text NOT NULL,
  source_name text NOT NULL,
  target_name text NOT NULL,
  group_label text NOT NULL DEFAULT '',
  excerpt text NOT NULL DEFAULT '',
  state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','sending','sent','dead')),
  attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_attempt_at timestamptz NOT NULL,
  lease_until timestamptz,
  bot_channel_id text NOT NULL DEFAULT '',
  feishu_message_id text NOT NULL DEFAULT '',
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
  CHECK (octet_length(excerpt) <= 1024)
);
CREATE INDEX np_agent_feishu_copy_due
  ON np_agent_feishu_copy_outbox(state,next_attempt_at,created_at,event_id);
CREATE INDEX np_agent_feishu_copy_reply
  ON np_agent_feishu_copy_outbox(bot_channel_id,feishu_message_id)
  WHERE state='sent';
