package hosted

// T-G4-01 · G4 托管 PG 数据模型（新平台，直接生 PG——方法件 8.2：V1 起新表不再写 SQLite 版）。
// 约定沿 internal/datad：SchemaDDL 常量＋CREATE IF NOT EXISTS 幂等 apply；
// 集成测试以 HUB_TEST_PG_DSN 门控（永不指生产库，与 DATAD_TEST_PG_DSN 同一纪律）。
//
// 命名：本包表一律 g4_ 前缀，避免与 Hub 主库（R0 迁移中）及 G3 identity 表冲突。
// hosted_seat 以 identity 的 agent_id 为主键（逻辑外键，跨服务边界不建物理 FK——
// 名称有效性与唯一性由 G3 IssueHostedAgent 判，G3 重审③）。

const SchemaDDL = `
-- 托管节点（G1 准备的宿主；节点代理登记）
CREATE TABLE IF NOT EXISTS g4_node (
  id            TEXT PRIMARY KEY,             -- nd_<hex16>
  name          TEXT NOT NULL UNIQUE,
  labels        JSONB NOT NULL DEFAULT '{}',  -- {site, spec, pool}
  state         TEXT NOT NULL DEFAULT 'offline'
                CHECK (state IN ('online','offline')),
  capacity      JSONB NOT NULL DEFAULT '{}',  -- 最近一次上报：cpu/mem/disk 总量与余量
  docker_version TEXT NOT NULL DEFAULT '',
  node_protocol_version INT NOT NULL DEFAULT 1,
  last_seen_at  timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now());

-- 节点凭据：只存哈希（轮换/撤销经 rotated_at/revoked_at；发行即旧替新）
CREATE TABLE IF NOT EXISTS g4_node_credential (
  node_id     TEXT PRIMARY KEY REFERENCES g4_node(id),
  secret_hash TEXT NOT NULL,
  issued_by   TEXT NOT NULL,
  issued_at   timestamptz NOT NULL DEFAULT now(),
  rotated_at  timestamptz,
  revoked_at  timestamptz);

-- 团队 LLM 账号池（C1：团队资源；个人凭据不进平台）
CREATE TABLE IF NOT EXISTS g4_account_pool (
  id         TEXT PRIMARY KEY,            -- acct_<hex16>
  name       TEXT NOT NULL UNIQUE,
  kind       TEXT NOT NULL CHECK (kind IN ('api_key','token_file')),
  cli        TEXT NOT NULL,
  llm        TEXT NOT NULL,
  secret_enc BYTEA NOT NULL,              -- 团队数据密钥加密的密文（信封加密）
  key_version INT NOT NULL,               -- 团队数据密钥版本号
  last4      TEXT NOT NULL DEFAULT '',    -- 登记时算好存下——console 只显示后四位，永不为此解密
  state      TEXT NOT NULL DEFAULT 'available'
             CHECK (state IN ('available','bound','disabled')),
  bound_agent_id TEXT,                    -- 一席一账号；NULL＝在池可分配
  created_by TEXT NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now());

-- 账号可用范围：按用户授权（组长定 da622016）
CREATE TABLE IF NOT EXISTS g4_account_grant (
  account_id TEXT NOT NULL REFERENCES g4_account_pool(id),
  user_lc    TEXT NOT NULL,
  granted_by TEXT NOT NULL,
  granted_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, user_lc));

-- 托管席位：以 identity 的 agent_id 为主键（不另立席位身份与名称空间）
CREATE TABLE IF NOT EXISTS g4_hosted_seat (
  agent_id    TEXT PRIMARY KEY,           -- identity.agents 的 id（ag_<hex16>）
  node_id     TEXT NOT NULL REFERENCES g4_node(id),
  cli         TEXT NOT NULL,
  llm         TEXT NOT NULL,
  cred_kind   TEXT NOT NULL CHECK (cred_kind IN ('api_key','token_file')),
  account_ref TEXT REFERENCES g4_account_pool(id),
  plan        TEXT NOT NULL CHECK (plan IN ('2c4g','4c8g')),
  state       TEXT NOT NULL DEFAULT 'queued'
              CHECK (state IN ('queued','pulling','starting','online','busy',
                               'disabled','stopped','error','destroyed')),
  state_detail TEXT NOT NULL DEFAULT '',  -- 机器码（契约 v2 错误码表）
  image_ref   TEXT NOT NULL DEFAULT '',   -- 当前镜像 ref（含 digest）
  created_by  TEXT NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now());

-- 生命周期事件（状态机轨迹；审计的载体之一）
CREATE TABLE IF NOT EXISTS g4_seat_event (
  id       BIGSERIAL PRIMARY KEY,
  agent_id TEXT NOT NULL REFERENCES g4_hosted_seat(agent_id),
  kind     TEXT NOT NULL,   -- create/pull/start/online/upgrade/restart/disable/enable/destroy/fail
  detail   JSONB,
  at       timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS g4_seat_event_agent_idx ON g4_seat_event(agent_id, id);

-- 用户配额（管理层设；新上限低于已用量允许保存、只限新建——G6）
CREATE TABLE IF NOT EXISTS g4_quota (
  user_lc       TEXT PRIMARY KEY,
  agents_limit  INT    NOT NULL DEFAULT 0,   -- 默认零（报业主项 2 的默认口径）
  mem_mb_limit  BIGINT NOT NULL DEFAULT 0,
  disk_mb_limit BIGINT NOT NULL DEFAULT 0,
  vcpu_limit    INT    NOT NULL DEFAULT 0,
  updated_by    TEXT NOT NULL,
  updated_at    timestamptz NOT NULL DEFAULT now());

-- 团队数据密钥（信封加密的中层）：库里只存被主密钥包裹后的密文＋版本号；主密钥不进库
CREATE TABLE IF NOT EXISTS g4_team_key (
  version    INT PRIMARY KEY,
  wrapped    BYTEA NOT NULL,              -- 主密钥包裹后的团队数据密钥
  state      TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','retired')),
  created_by TEXT NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now());

-- G4 审计（账号登记/改密/授权变更/解密下发/绑定解绑/生命周期；actor/action/target 三段）
CREATE TABLE IF NOT EXISTS g4_audit (
  id     BIGSERIAL PRIMARY KEY,
  actor  TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  detail JSONB,
  at     timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS g4_audit_action_idx ON g4_audit(action, id);
`

// DropDDL 仅测试用：逆向拆除（rollback 的可重复性验证载体）。
const DropDDL = `
DROP TABLE IF EXISTS g4_audit, g4_team_key, g4_quota, g4_account_grant, g4_account_pool,
  g4_seat_event, g4_hosted_seat, g4_node_credential, g4_node CASCADE;
`
