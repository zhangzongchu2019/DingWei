-- FEISHU-COPY independent projection outbox. Filename is assigned after PG ID allocation.
CREATE TABLE np_extseat_feishu_copy (
  event_id text PRIMARY KEY,
  seat_id text NOT NULL REFERENCES np_extseat(agent_id),
  principal_issuer text NOT NULL,
  principal_subject text NOT NULL,
  external_name text NOT NULL,
  target_name text NOT NULL,
  owner_display_name text NOT NULL,
  owner_short_name text NOT NULL,
  direction text NOT NULL CHECK (direction IN ('external_to_internal','internal_to_external')),
  body text NOT NULL,
  state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','sending','sent','dead')),
  attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_attempt_at timestamptz NOT NULL,
  lease_until timestamptz,
  bot_channel_id text NOT NULL DEFAULT '',
  feishu_message_id text NOT NULL DEFAULT '',
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL CHECK (expires_at > created_at)
);
CREATE INDEX np_extseat_feishu_copy_due ON np_extseat_feishu_copy(state,next_attempt_at,created_at,event_id);
CREATE INDEX np_extseat_feishu_copy_reply ON np_extseat_feishu_copy(bot_channel_id,feishu_message_id) WHERE state='sent';
