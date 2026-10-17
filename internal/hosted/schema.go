package hosted

// T-G4-01 · G4 托管 PG 数据模型（新平台，直接生 PG——方法件 8.2：V1 起新表不再写 SQLite 版）。
// 约定沿 internal/datad：SchemaDDL 常量＋幂等 apply；建表 CREATE IF NOT EXISTS、
// 加列 ALTER ADD COLUMN IF NOT EXISTS（g4_ 首次加列＝包 B2 的 model，R-V2-SEAT-B2-SCHEMA 裁定）；
// 集成测试以 HUB_TEST_PG_DSN 门控（永不指生产库，与 DATAD_TEST_PG_DSN 同一纪律）。
//
// 命名：本包表一律 g4_ 前缀，避免与 Hub 主库（R0 迁移中）及 G3 identity 表冲突。
// hosted_seat 以 identity 的 agent_id 为主键（逻辑外键，跨服务边界不建物理 FK——
// 名称有效性与唯一性由 G3 IssueHostedAgent 判，G3 重审③）。

const SchemaDDL = `
-- Durable two-recipient notification outbox. Node event receipt and notices are
-- committed in one transaction before the node is allowed to discard its event.
CREATE TABLE IF NOT EXISTS g4_quota_notice (
  node_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  recipient TEXT NOT NULL CHECK (recipient IN ('owner','ops')),
  seat_id TEXT NOT NULL,
  reason TEXT NOT NULL,
  writer_kind TEXT NOT NULL DEFAULT '',
  measured_at timestamptz NOT NULL,
  delivered_at timestamptz,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (node_id,event_id,recipient));
CREATE INDEX IF NOT EXISTS g4_quota_notice_pending_idx ON g4_quota_notice(next_attempt_at,measured_at) WHERE delivered_at IS NULL;
CREATE INDEX IF NOT EXISTS g4_quota_notice_node_idx ON g4_quota_notice(node_id,delivered_at);

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

-- LLM 账号池。🔴 0.2.1.101 业主新裁（10-06，R-V2-ACCOUNT-PER-USER）：账号按人隔离（owner_lc），个人凭据进平台，
-- 以团队数据密钥加密——平台持有者（主密钥持有人）技术上可解密；原「团队资源；个人凭据不进平台」作废。
CREATE TABLE IF NOT EXISTS g4_account_pool (
  id         TEXT PRIMARY KEY,            -- acct_<hex16>
  name       TEXT NOT NULL,                -- 本人内唯一（g4_account_pool_owner_name），不再全局唯一
  kind       TEXT NOT NULL CHECK (kind IN ('api_key','token_file')),
  cli        TEXT NOT NULL,
  llm        TEXT NOT NULL,
  secret_enc BYTEA NOT NULL,              -- 团队数据密钥加密的密文（信封加密）
  key_version INT NOT NULL,               -- 团队数据密钥版本号
  last4      TEXT NOT NULL DEFAULT '',    -- 登记时算好存下——console 只显示后四位，永不为此解密
  state      TEXT NOT NULL DEFAULT 'available'
             CHECK (state IN ('available','bound','disabled')),
  bound_agent_id TEXT,                    -- 历史列（0.2.1.101 起不再写）：占用改以 g4_hosted_seat.account_ref 为准
  created_by TEXT NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now());

-- 账号可用范围：按用户授权（组长定 da622016）。0.2.1.101 起不再读写（归属改 owner_lc），留作历史与回退兼容
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
  plan        TEXT NOT NULL CHECK (plan IN ('1c1.5g40','2c4g','4c8g')),
  model       TEXT NOT NULL DEFAULT '', -- 生效模型（包 B2 修订 3：建席时定死、落库；upgrade/restart 按记录值重算）
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

-- 生命周期异步任务：独立 job_id 防止同一席位的多轮操作互相串读。
CREATE TABLE IF NOT EXISTS g4_lifecycle_job (
  id         TEXT PRIMARY KEY,
  agent_id   TEXT NOT NULL REFERENCES g4_hosted_seat(agent_id),
  created_by TEXT NOT NULL,
  op         TEXT NOT NULL CHECK (op IN ('upgrade','restart','disable','enable','destroy','refresh_cred')),
  args       JSONB NOT NULL DEFAULT '{}',
  stage      TEXT NOT NULL DEFAULT 'queued'
             CHECK (stage IN ('queued','running','completed','failed')),
  reason     TEXT NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS g4_lifecycle_job_agent_idx ON g4_lifecycle_job(agent_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS g4_lifecycle_job_one_active_idx ON g4_lifecycle_job(agent_id)
  WHERE stage IN ('queued','running');

-- 用户配额（管理层设；新上限低于已用量允许保存、只限新建——G6）
CREATE TABLE IF NOT EXISTS g4_quota (
  user_lc       TEXT PRIMARY KEY,
  agents_limit  INT    NOT NULL DEFAULT 4,      -- 业主 8971bddf：默认 Agent 数 4
  mem_mb_limit  BIGINT NOT NULL DEFAULT 16384,  -- 默认内存 16GB
  disk_mb_limit BIGINT NOT NULL DEFAULT 65536,  -- 默认磁盘 64GB
  vcpu_limit    INT    NOT NULL DEFAULT 4,      -- 默认 vCPU 4
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

-- 销毁备份登记（T-G4-53 备份管线：节点出卷→Hub 收落 dw 的 /data→14 天到期删；不落节点盘）
CREATE TABLE IF NOT EXISTS g4_backup (
  id          TEXT PRIMARY KEY,             -- bk_<hex16>
  node_id     TEXT NOT NULL,
  seat_id     TEXT NOT NULL,
  state       TEXT NOT NULL DEFAULT 'pending'
              CHECK (state IN ('pending','complete','expired')),
  path        TEXT NOT NULL DEFAULT '',     -- Hub 本地落盘路径（dw 的 /data 下）
  size_bytes  BIGINT NOT NULL DEFAULT 0,
  expires_at  timestamptz NOT NULL,         -- 保留 14 天到期删（业主 8971bddf）
  created_at  timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS g4_backup_state_idx ON g4_backup(state, expires_at);

-- 包 B2（R-V2-SEAT-B2-SCHEMA-20261005002126）：g4_ 首次加列。生产已有 g4_hosted_seat（托管面已开、
-- 业主席位在跑）⇒ CREATE IF NOT EXISTS 不会加列，须幂等 ALTER；存量行 model='' ＝ 走节点默认。
ALTER TABLE g4_hosted_seat ADD COLUMN IF NOT EXISTS model TEXT NOT NULL DEFAULT '';

-- 2026-10-17: add the minimum plan without rewriting existing seats or quotas.
-- ApplySchema executes this DDL in one implicit transaction, including replacement
-- of the deployed inline CHECK; CREATE IF NOT EXISTS alone cannot upgrade it.
ALTER TABLE g4_hosted_seat DROP CONSTRAINT IF EXISTS g4_hosted_seat_plan_check;
ALTER TABLE g4_hosted_seat ADD CONSTRAINT g4_hosted_seat_plan_check
  CHECK (plan IN ('1c1.5g40','2c4g','4c8g'));

-- 0.2.1.101 账号包（R-V2-ACCOUNT-PER-USER）。整段 SchemaDDL 以单条多语句执行＝PG 隐式单事务：
-- 下面的 DROP＋ADD 不会留下「无约束」窗口。全部幂等，每次启动执行。
-- ① 归属：owner_lc＝identity.User.UserIDLC()（与 N3.Actor、席位 created_by 同形）；无主行＝''（任何人不可见）。
ALTER TABLE g4_account_pool ADD COLUMN IF NOT EXISTS owner_lc TEXT NOT NULL DEFAULT '';
-- ② 名字：全局唯一 → 本人内唯一（全局唯一会让「名称已存在」泄露他人账号名）。
ALTER TABLE g4_account_pool DROP CONSTRAINT IF EXISTS g4_account_pool_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS g4_account_pool_owner_name ON g4_account_pool(owner_lc, name);
CREATE INDEX IF NOT EXISTS g4_account_pool_owner ON g4_account_pool(owner_lc);
-- ③ 存量回填：恰有一条授权的账号 ⇒ 归该用户；已有属主的行不动（owner_lc='' 条件）；多授权／无授权保持无主。
UPDATE g4_account_pool p SET owner_lc = g.user_lc
  FROM (SELECT account_id, min(user_lc) AS user_lc, count(*) AS n FROM g4_account_grant GROUP BY account_id) g
 WHERE p.id = g.account_id AND g.n = 1 AND p.owner_lc = '';
-- ④ 生命周期 job 加 refresh_cred（刷新凭据，R-V2-ACCOUNT-REFRESH-CRED）：存量表的内联 CHECK 换新。
ALTER TABLE g4_lifecycle_job DROP CONSTRAINT IF EXISTS g4_lifecycle_job_op_check;
ALTER TABLE g4_lifecycle_job ADD CONSTRAINT g4_lifecycle_job_op_check
  CHECK (op IN ('upgrade','restart','disable','enable','destroy','refresh_cred'));

-- 0.2.1.101 账号登录（R-V2-ACCOUNT-LOGIN-FLOW，codex 设备码）：登录会话。链接与码只在进行中保留，结束即清空；
-- 凭证永不入本表（节点主动上传后直接加密入池）。id＝32 位十六进制随机（上传端点的不可猜部分）。
CREATE TABLE IF NOT EXISTS g4_account_login (
  id          TEXT PRIMARY KEY,
  owner_lc    TEXT NOT NULL,
  cli         TEXT NOT NULL,
  llm         TEXT NOT NULL,
  email       TEXT NOT NULL,
  account_id  TEXT,                       -- 重新登录的目标账号（新登记为 NULL）
  node_id     TEXT NOT NULL,              -- 执行节点（上传只认它）
  state       TEXT NOT NULL CHECK (state IN ('starting','waiting_user','completing','done','failed','expired','cancelled')),
  url         TEXT NOT NULL DEFAULT '',
  user_code   TEXT NOT NULL DEFAULT '',
  reason      TEXT NOT NULL DEFAULT '',
  result_account_id TEXT,
  created_at  timestamptz NOT NULL DEFAULT now(),
  expires_at  timestamptz NOT NULL,
  finished_at timestamptz);
CREATE INDEX IF NOT EXISTS g4_account_login_owner ON g4_account_login(owner_lc, created_at);
CREATE INDEX IF NOT EXISTS g4_account_login_active ON g4_account_login(state) WHERE state IN ('starting','waiting_user','completing');
-- 每人同一时刻至多 1 个进行中登录（库层保证，不靠先查后插）。
CREATE UNIQUE INDEX IF NOT EXISTS g4_account_login_one_active ON g4_account_login(owner_lc) WHERE state IN ('starting','waiting_user','completing');
-- completing＝上传已被原子认领、正在入池（清扫与取消不碰它；认领超过 2 分钟仍未收尾或 Hub 重启 ⇒ failed/interrupted）。
ALTER TABLE g4_account_login ADD COLUMN IF NOT EXISTS claimed_at timestamptz;
-- 幂等升级（复核第 4 批建议 2）：若某库曾应用过早期定义（CHECK 无 completing、索引旧谓词），在此换新；同一隐式事务。
ALTER TABLE g4_account_login DROP CONSTRAINT IF EXISTS g4_account_login_state_check;
ALTER TABLE g4_account_login ADD CONSTRAINT g4_account_login_state_check
  CHECK (state IN ('starting','waiting_user','completing','done','failed','expired','cancelled'));
DROP INDEX IF EXISTS g4_account_login_one_active;
CREATE UNIQUE INDEX g4_account_login_one_active ON g4_account_login(owner_lc) WHERE state IN ('starting','waiting_user','completing');
DROP INDEX IF EXISTS g4_account_login_active;
CREATE INDEX g4_account_login_active ON g4_account_login(state) WHERE state IN ('starting','waiting_user','completing');
-- 账号登录入池的账号记邮箱（显示用；个人信息——只给属主本人，管理员列表不给）。
ALTER TABLE g4_account_pool ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT '';
`

// DropDDL 仅测试用：逆向拆除（rollback 的可重复性验证载体）。
// 🔴 表名集合必须与 SchemaDDL 一致（R-V2-DEPLOY-ARTIFACTS 甲案补拆 g4_backup；
// 漏拆由 TestSchemaDropLeavesNoG4Tables 钉住——少一张即红）。
const DropDDL = `
DROP TABLE IF EXISTS g4_quota_notice, g4_account_login, g4_audit, g4_team_key, g4_quota, g4_backup, g4_lifecycle_job, g4_account_grant, g4_account_pool,
  g4_seat_event, g4_hosted_seat, g4_node_credential, g4_node CASCADE;
`
