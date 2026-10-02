-- 0400 · fanout 基线（G2 fanout 域的冻结副本，G2 代管账本）
-- 冻结自 internal/fanout/pgschema.sql ＠ 0228e0d4（R-G2-FANOUT-HARDEN ①②：五表建表收口到该文件、
-- 时间列 INTEGER→BIGINT；裁定 RULING-REGISTER 2026-10-02T09:41:37Z；源文件 sha256 cc6a60e0f5e8d7cd5ba941e176923eefe228417a53761effbfb8d556ed60218d）。
-- 「── 以下为冻结正文 ──」那一行【之后】与该文件逐字相同（可用 diff 核，勿手改）。
--
-- 🔴 号段 0400-0499 ＝ fanout 专用（本条是该段首条）。原先口径页 §五 只分到 0300-0399（G5 view），
--    fanout 无段；本次新增并回写口径页 §五。🔴 段号只解决撞号，不解决依赖顺序：
--    本条只依赖 0001（不与 identity 0100／hosted 0200／view 0300 互相引用）。
-- 🔴 本条是【冻结副本】，不是活引用：一旦在长命库上记过账，正文即不可变（改了 digest 就对不上，
--    runner 会拒绝启动 —— 那正是 internal/store/sqlite.go:186-193 用两次事故换来的语义）。
--    fanout 域此后改自己的 pgschema.sql（那是给临时库与 OpenSpool 用的幂等 DDL）时，
--    🔴 不要顺手改本条，而是【新增一条】0401_fanout_*.sql 写增量（ALTER／CREATE INDEX …）。
--    漂移由 internal/store/migrate 的双轨指纹用例守着：
--    「空库经各域 SchemaDDL 建出的形状 ≡ 空库经账本 Migrate 建出的形状」，漂移即在 CI 上红。
-- 🔵 与 0100 的差别：身份域那条依赖 0001 的 audit_log（审计复用），本条【不依赖】任何既有表 ——
--    五表自成一组，无外键指向 np_*／audit_log。selfcheck 也在内（pre-arm③ 的写读删靶表，
--    原由 admin.go 懒建，收口后进本条；它无时间列）。
-- 🔵 时间列一律 BIGINT（Unix 秒，值由 Go 侧写入，无 now() 默认值）；理由写在下面正文的头注里
--    （2038 溢出对 next_retry／created_at 是【静默改序】而不是报错）。
--
-- ── 以下为冻结正文 ──────────────────────────────────────────────────────
-- fanout spool PG 表结构（G2；五表，组长裁定 R-G2-FANOUT-HARDEN／RULING-REGISTER 2026-10-02T09:41:37Z）
-- 本文件是 fanout 建表的【唯一来源】：产品侧由 OpenSpool 一次无参 Exec 施加（幂等），
-- 迁移账本另持一份冻结副本 internal/store/migrate/migrations/0400_fanout_baseline.sql（号段见口径页 §五）。
-- 🔴 改本文件之后【不要】回去改 0400 的正文（已施加条目不可变，改一个注释字 digest 就对不上，
--    runner 会拒绝启动）—— 正确做法是【新增一条】0401_fanout_*.sql 写增量（ALTER／CREATE INDEX …）。
--    源与账本的漂移由 internal/store/migrate 的双轨形状格守着：漂移即在 CI 上红。
-- 🔴 时间列一律 BIGINT（存 Unix 秒）：原为 INTEGER，2038-01-19T03:14:07Z 之后溢出为负，
--    而 next_retry／created_at 是队列取件的比较键（WHERE next_retry<=$1 ORDER BY created_at），
--    溢出后不是报错而是【静默改序】—— 到期件排到队尾、DLQ 退避窗算错。故按裁定改宽。
-- 🔴 时间值一律由 Go 侧写入（time.Now().UTC().Unix()），本文件不出现 now() 默认值：
--    默认值会让「落盘时刻」由库决定，契约里注入的时刻就测不到它。
-- 🔵 attempts 保持 INTEGER：它是次数不是时刻，8 次上限（Config.MaxAttempts）离 2^31 很远。
-- 🔵 selfcheck 无时间列（pre-arm③ 只做写读删一轮，证 fsync 路活），故本文件里没有它的时间列可改。
-- 🔵 单实例假设：custodyWorker 取件即置 sending（乐观置忙，无 SELECT … FOR UPDATE），
--    只在【单实例部署】下成立；多副本会重复投递。该约束写在
--    runbooks/SPEC-NEWPLAT-G2-FANOUT-PG-DRAFT.md，🔴 本文件的 DDL 不假装能防它。

CREATE TABLE IF NOT EXISTS outbound(
  outbound_id TEXT PRIMARY KEY,
  caller_id   TEXT NOT NULL,
  request_id  TEXT NOT NULL,
  body_hash   TEXT NOT NULL,
  body        BYTEA NOT NULL,
  status      TEXT NOT NULL DEFAULT 'queued',
  attempts    INTEGER NOT NULL DEFAULT 0,
  next_retry  BIGINT NOT NULL DEFAULT 0,
  lark_msg_id TEXT NOT NULL DEFAULT '',
  last_error  TEXT NOT NULL DEFAULT '',
  created_at  BIGINT NOT NULL,
  updated_at  BIGINT NOT NULL,
  UNIQUE(caller_id, request_id));

CREATE TABLE IF NOT EXISTS inbound_msg(
  msg_key    TEXT PRIMARY KEY,
  body       BYTEA NOT NULL,
  status     TEXT NOT NULL DEFAULT 'pending',
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL);

CREATE TABLE IF NOT EXISTS inbound_sink(
  msg_key    TEXT NOT NULL,
  sink_name  TEXT NOT NULL,
  role       TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'queued',
  attempts   INTEGER NOT NULL DEFAULT 0,
  next_retry BIGINT NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  updated_at BIGINT NOT NULL,
  PRIMARY KEY(msg_key, sink_name));

CREATE TABLE IF NOT EXISTS waive_audit(
  msg_key   TEXT NOT NULL,
  sink_name TEXT NOT NULL,
  actor     TEXT NOT NULL,
  reason    TEXT NOT NULL,
  at        BIGINT NOT NULL);

CREATE TABLE IF NOT EXISTS selfcheck(k TEXT PRIMARY KEY, v TEXT);
