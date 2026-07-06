-- WorkPulse 初始 schema（全部配置 + 消息入 SQLite，见规范 §5/§13.2）。
-- 约束/唯一键参考规范 §15.5。

-- ===== 消息总线（M0）=====
CREATE TABLE IF NOT EXISTS bot_channel (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  app_id      TEXT NOT NULL,
  purpose     TEXT NOT NULL DEFAULT 'general', -- dm|group|general
  can_send    INTEGER NOT NULL DEFAULT 1,
  can_receive INTEGER NOT NULL DEFAULT 1,
  active      INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS chat_entity (
  id             TEXT PRIMARY KEY,
  bot_channel_id TEXT NOT NULL,
  type           TEXT NOT NULL,            -- personal|group
  feishu_id      TEXT NOT NULL,            -- 个人=open_id / 群=chat_id
  display_name   TEXT,
  bound_owner    TEXT,
  active         INTEGER NOT NULL DEFAULT 1,
  UNIQUE(bot_channel_id, feishu_id)        -- §15.5：id 随渠道而定
);

CREATE TABLE IF NOT EXISTS contact_cache (
  feishu_id  TEXT NOT NULL,
  id_type    TEXT NOT NULL,                -- chat_id|open_id|user_id
  name       TEXT,
  updated_at TEXT,
  PRIMARY KEY(feishu_id, id_type)
);

CREATE TABLE IF NOT EXISTS message (
  id             TEXT PRIMARY KEY,
  chat_entity_id TEXT NOT NULL,
  direction      TEXT NOT NULL,            -- in|out
  bot_channel_id TEXT NOT NULL,
  feishu_msg_id  TEXT,
  chat_type      TEXT,                     -- personal|group
  sender_open_id TEXT,
  content_json   TEXT,                     -- 完整内容（全部入库）
  status         TEXT NOT NULL,
  attempts       INTEGER NOT NULL DEFAULT 0,
  error          TEXT,
  created_at     TEXT NOT NULL,
  processed_at   TEXT,
  UNIQUE(bot_channel_id, feishu_msg_id)    -- §15.5：幂等去重
);
CREATE INDEX IF NOT EXISTS idx_message_entity_status ON message(chat_entity_id, status);

-- ===== 服务注册（路由平台，M8）=====
CREATE TABLE IF NOT EXISTS registered_service (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  description   TEXT,
  delivery_type TEXT NOT NULL,             -- ws|http|process|queue|inproc（字头路由必须 ws）
  endpoint      TEXT,
  secret_ref    TEXT,
  reply_mode    TEXT NOT NULL DEFAULT 'sync', -- sync|async|none
  priority      INTEGER NOT NULL DEFAULT 100,
  timeout_ms    INTEGER NOT NULL DEFAULT 5000,
  retry         INTEGER NOT NULL DEFAULT 0,
  enabled       INTEGER NOT NULL DEFAULT 1,
  health_status TEXT,
  last_heartbeat TEXT
);

CREATE TABLE IF NOT EXISTS routing_rule (
  id                TEXT PRIMARY KEY,
  service_id        TEXT NOT NULL,
  match_type        TEXT NOT NULL,         -- prefix|symbol|keyword|regex|command|entity|channel|llm_category
  match_expr        TEXT NOT NULL,         -- prefix 支持 ?/* 通配 + ;/；多字头列表（OR），见 §15.1
  combine           TEXT NOT NULL DEFAULT 'or',
  priority          INTEGER NOT NULL DEFAULT 100,
  scope_entity_type TEXT,
  account_scope_json TEXT,                 -- 须 ⊆ api key 绑定账号
  case_sensitive    INTEGER NOT NULL DEFAULT 0,
  strip_prefix      INTEGER NOT NULL DEFAULT 0,
  enabled           INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS service_api_key (
  id         TEXT PRIMARY KEY,
  service_id TEXT NOT NULL,
  key_hash   TEXT NOT NULL UNIQUE,
  label      TEXT,
  active     INTEGER NOT NULL DEFAULT 1,
  created_at TEXT,
  revoked_at TEXT
);

CREATE TABLE IF NOT EXISTS api_key_account (
  api_key_id     TEXT NOT NULL,
  chat_entity_id TEXT NOT NULL,
  PRIMARY KEY(api_key_id, chat_entity_id)
);

-- ===== 业务（M1-M7）=====
CREATE TABLE IF NOT EXISTS member (
  id              TEXT PRIMARY KEY,
  owner_key       TEXT NOT NULL UNIQUE,
  display_name    TEXT,
  feishu_open_id  TEXT,
  role            TEXT NOT NULL DEFAULT 'member', -- member|collaborator|manager
  evidence_optout INTEGER NOT NULL DEFAULT 0,
  dm_optout       INTEGER NOT NULL DEFAULT 0,
  active          INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS schedule (
  id         TEXT PRIMARY KEY,
  owner_key  TEXT NOT NULL,
  start_date TEXT NOT NULL,
  end_date   TEXT NOT NULL,
  task       TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'planned',
  priority   INTEGER NOT NULL DEFAULT 100,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(owner_key, start_date, task)       -- §15.5：业务去重
);

CREATE TABLE IF NOT EXISTS progress (
  id          TEXT PRIMARY KEY,
  owner_key   TEXT NOT NULL,
  task_key    TEXT NOT NULL,
  note        TEXT,
  percent     INTEGER,
  reported_at TEXT NOT NULL,
  source      TEXT NOT NULL DEFAULT 'self'  -- self|ai_evidence
);

CREATE TABLE IF NOT EXISTS risk (
  id               TEXT PRIMARY KEY,
  owner_key        TEXT,
  content          TEXT NOT NULL,
  status           TEXT NOT NULL DEFAULT 'open',
  related_task_key TEXT,
  reporters_json   TEXT,
  created_at       TEXT NOT NULL,
  resolved_at      TEXT
);

CREATE TABLE IF NOT EXISTS ai_evidence (
  id             TEXT PRIMARY KEY,
  owner_key      TEXT NOT NULL,
  session_id     TEXT,
  session_source TEXT,
  work_item      TEXT,
  artifact       TEXT,
  files_json     TEXT,
  action_type    TEXT,
  occurred_at    TEXT,
  mapped_task_key TEXT,
  confidence     REAL,
  raw_excerpt_hash TEXT
);

CREATE TABLE IF NOT EXISTS reconciliation (
  id           TEXT PRIMARY KEY,
  owner_key    TEXT NOT NULL,
  task_key     TEXT NOT NULL,
  self_status  TEXT,
  evidence_count INTEGER,
  verdict      TEXT,                        -- confirmed|partial|no_evidence|suspected_lag|ahead
  computed_at  TEXT,
  detail_json  TEXT
);

CREATE TABLE IF NOT EXISTS pending_update (
  id         TEXT PRIMARY KEY,
  open_id    TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'pending', -- pending|confirmed|cancelled|expired §15.2
  created_at TEXT NOT NULL,
  expires_at TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_pending_open ON pending_update(open_id) WHERE status='pending';

-- ===== 配置 / 管理 / 审计 =====
CREATE TABLE IF NOT EXISTS app_config (
  key        TEXT PRIMARY KEY,
  value_json TEXT,
  updated_at TEXT
);

CREATE TABLE IF NOT EXISTS admin_user (
  id            TEXT PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,             -- 哈希，不存明文 §M9
  active        INTEGER NOT NULL DEFAULT 1,
  last_login_at TEXT,
  created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS admin_login_attempt (
  username     TEXT NOT NULL,
  ip           TEXT NOT NULL,
  failed_count INTEGER NOT NULL DEFAULT 0,
  window_start TEXT,
  PRIMARY KEY(username, ip)
);

CREATE TABLE IF NOT EXISTS audit_log (
  id          TEXT PRIMARY KEY,
  actor       TEXT,
  action      TEXT NOT NULL,
  target      TEXT,
  before_json TEXT,
  after_json  TEXT,
  ts          TEXT NOT NULL
);
