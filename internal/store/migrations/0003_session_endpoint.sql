-- M8/M10 统一寻址会话端点。key_id 是公开地址标识；连接 secret 只存 service_api_key.key_hash。
CREATE TABLE IF NOT EXISTS session_endpoint (
  key_id         TEXT NOT NULL,
  session_name   TEXT NOT NULL,
  last_seen_at    TEXT NOT NULL,
  active          INTEGER NOT NULL DEFAULT 1,
  mirror_enabled  INTEGER NOT NULL DEFAULT 0,
  mirror_to       TEXT,
  PRIMARY KEY(key_id, session_name)
);
