-- 0100 · identity 基线（G3 身份域的冻结副本，G2 代管账本）
-- 冻结自 internal/identity/pgschema.sql ＠ dd69f88a（源文件 sha256 1f99814ca49a215bcff7733eff3cba25003185f103b3b394beb5a22215de7a4b）。
-- 「── 以下为冻结正文 ──」那一行【之后】与该文件逐字相同（可用 diff 核，勿手改）。
--
-- 🔴 本条是【冻结副本】，不是活引用：一旦在长命库上记过账，正文即不可变（改了 digest 就对不上，
--    runner 会拒绝启动 —— 那正是 internal/store/sqlite.go:186-193 用两次事故换来的语义）。
--    身份域此后改自己的 pgschema.sql（那是给临时库用的幂等 DDL）时，🔴 不要顺手改本条，
--    而是【新增一条】0101_identity_*.sql 写增量（ALTER／CREATE INDEX …）。
--    漂移由 internal/store/migrate 的双轨指纹用例守着：
--    「空库经各域 SchemaDDL 建出的形状 ≡ 空库经账本 Migrate 建出的形状」，漂移即在 CI 上红。
-- 🔵 依赖 0001：审计复用 audit_log，身份域自己不建（下面原文件头注同一句）。
--
-- ── 以下为冻结正文 ──────────────────────────────────────────────────────
-- 身份存储 PG 表结构（G3；组长台账 43342da4：新领域在自己包内给 PG 实现，迁移并入 G2 的迁移账本）
-- 设计件 runbooks/DESIGN-G3-ENROLL-AUTH-API-20261001.md 第一节；表名与建议件 0efb5e3e073e 一致，state 加 suspended（台账 4ecd2246）。
-- 🔴 时间一律由接口传入（契约用注入时刻测），本文件不出现 now() 默认值。
-- 🔵 审计复用既有 audit_log（不在本文件建），由 G2 的迁移账本保证其先于本文件存在。

CREATE TABLE IF NOT EXISTS np_user (
    user_id_lc           text        PRIMARY KEY,
    user_id              text        NOT NULL,
    short_name           text        NOT NULL,
    short_name_lc        text        NOT NULL UNIQUE,
    display_name         text        NOT NULL DEFAULT '',
    type                 text        NOT NULL CHECK (type IN ('platform_admin', 'manager', 'member')),
    state                text        NOT NULL CHECK (state IN ('pending_bind', 'active', 'disabled')),
    password_hash        text        NOT NULL DEFAULT '',
    must_change_password boolean     NOT NULL DEFAULT false,
    created_at           timestamptz NOT NULL
);

-- 一个渠道身份只属于一个用户（主键即唯一约束）。
CREATE TABLE IF NOT EXISTS np_identity (
    provider   text NOT NULL,
    issuer     text NOT NULL,
    subject    text NOT NULL,
    user_id_lc text NOT NULL REFERENCES np_user (user_id_lc),
    PRIMARY KEY (provider, issuer, subject)
);

CREATE TABLE IF NOT EXISTS np_registration_code (
    code_digest text        PRIMARY KEY,
    user_id_lc  text        NOT NULL REFERENCES np_user (user_id_lc),
    expires_at  timestamptz NOT NULL,
    redeemed_at timestamptz
);
CREATE INDEX IF NOT EXISTS np_registration_code_user ON np_registration_code (user_id_lc);

CREATE TABLE IF NOT EXISTS np_session (
    token_hash text        PRIMARY KEY,
    user_id_lc text        NOT NULL REFERENCES np_user (user_id_lc),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz
);
CREATE INDEX IF NOT EXISTS np_session_user ON np_session (user_id_lc);

-- owner_lc 不设外键：契约允许申请先于用户行存在（与内存实现同语义）；归属由业务层校验。
CREATE TABLE IF NOT EXISTS np_agent (
    id              text        PRIMARY KEY,
    owner_lc        text        NOT NULL,
    name            text        NOT NULL,
    name_lc         text        NOT NULL,
    group_code      text        NOT NULL DEFAULT '',
    origin          text        NOT NULL CHECK (origin IN ('selfhost', 'hosted')),
    state           text        NOT NULL CHECK (state IN ('active', 'suspended', 'revoked')),
    credential_hash text        UNIQUE,
    created_at      timestamptz NOT NULL,
    revoked_at      timestamptz,
    revoked_by      text        NOT NULL DEFAULT ''
);
-- 名字只在「未吊销」（active 或 suspended）范围内唯一：吊销即释放名字，暂停中仍占用。
CREATE UNIQUE INDEX IF NOT EXISTS np_agent_owner_name_current
    ON np_agent (owner_lc, name_lc) WHERE state IN ('active', 'suspended');

CREATE TABLE IF NOT EXISTS np_enroll_application (
    id                  text        PRIMARY KEY,
    owner_lc            text        NOT NULL,
    pairing_salt        text        NOT NULL DEFAULT '',
    pairing_digest      text        NOT NULL DEFAULT '',
    poll_token_hash     text        NOT NULL DEFAULT '',
    device_fingerprint  text        NOT NULL DEFAULT '',
    client_version      text        NOT NULL DEFAULT '',
    source_ip           text        NOT NULL DEFAULT '',
    requested_at        timestamptz NOT NULL,
    expires_at          timestamptz NOT NULL,
    state               text        NOT NULL CHECK (state IN ('pending', 'approved', 'rejected', 'superseded')),
    pairing_failures    integer     NOT NULL DEFAULT 0,
    agent_id            text        REFERENCES np_agent (id),
    credential_taken_at timestamptz
);
-- 审核区：按时刻、再按申请号升序（同一时刻顺序必须确定，契约 V1_pending_order_is_deterministic）。
CREATE INDEX IF NOT EXISTS np_enroll_application_pending
    ON np_enroll_application (owner_lc, state, requested_at, id);
