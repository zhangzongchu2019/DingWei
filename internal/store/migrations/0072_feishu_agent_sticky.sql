-- R14: metadata only. Identity authority remains in PG; no cross-store FK.
CREATE TABLE r14_feishu_sticky_state (
 user_id_lc TEXT NOT NULL,
 bot_channel_id TEXT NOT NULL,
 agent_id TEXT NOT NULL,
 generation INTEGER NOT NULL CHECK(generation > 0),
 selected_at_utc TEXT NOT NULL,
 expires_at_utc TEXT NOT NULL,
 last_source_message_id TEXT NOT NULL,
 updated_at_utc TEXT NOT NULL,
 PRIMARY KEY(user_id_lc, bot_channel_id)
);
CREATE INDEX r14_sticky_state_expiry ON r14_feishu_sticky_state(expires_at_utc);
CREATE INDEX r14_sticky_state_activity ON r14_feishu_sticky_state(updated_at_utc);
CREATE TABLE r14_feishu_sticky_operation (
 operation_seq INTEGER PRIMARY KEY AUTOINCREMENT,
 user_id_lc TEXT NOT NULL,
 bot_channel_id TEXT NOT NULL,
 source_message_id TEXT NOT NULL,
 operation_kind TEXT NOT NULL CHECK(operation_kind IN ('explicit','select-only','no-prefix')),
 base_generation INTEGER NOT NULL,
 result_generation INTEGER NOT NULL DEFAULT 0,
 target_agent_id TEXT NOT NULL DEFAULT '',
 decision TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL CHECK(status IN ('pending','unknown','accepted','rejected','stale_completion','resolved')),
 delivery_id TEXT NOT NULL DEFAULT '',
 source_envelope_id TEXT NOT NULL DEFAULT '',
 created_at_utc TEXT NOT NULL,
 completed_at_utc TEXT NOT NULL DEFAULT '',
 UNIQUE(bot_channel_id, source_message_id)
);
CREATE INDEX r14_sticky_operation_lane ON r14_feishu_sticky_operation(user_id_lc,bot_channel_id,operation_seq);
CREATE INDEX r14_sticky_operation_cleanup ON r14_feishu_sticky_operation(status,completed_at_utc);
CREATE INDEX r14_sticky_operation_pending ON r14_feishu_sticky_operation(status,created_at_utc,operation_seq);
