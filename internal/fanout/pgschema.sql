-- fanout spool PG 表结构（G2；五表，组长裁定 R-G2-FANOUT-HARDEN／RULING-REGISTER 2026-10-02T09:41:37Z）
-- 本文件是 fanout 五表 DDL 的【唯一真源】。谁施加它（裁定 R-G2-LEDGER-DOMAIN，2026-10-02T11:06:25Z）：
--   · 产品侧＝迁移账本条目 0400_fanout_baseline.sql（本文件的冻结副本，号段见口径页 §五），
--     由装配层 cmd/feishu-fanout 的 openSpoolPool 施加：migrate.Migrate(…, FanoutDomains())。
--     🔴 fanout 是独立服务、独立库，只施加 fanout 域（0400-0499）；Hub 那本账不进这个库，反之亦然。
--   · fanout.OpenSpool 只【核对】五表在不在（只读，一条 DDL 都不发）——它不再自建表。
--   · 临时库／用例夹具直接施加本文件的字节（internal/fanout 的 spoolTestPool）。
-- 🔵 本文件与 0400 的冻结正文自本次起在【头注】上不再逐字相同（本段改写过；DDL 部分逐字相同）。
--    这不是漂移：源 ↔ 账本的判据是【形状】（internal/store/migrate 的双轨形状格），
--    字节那一轨只管「已施加的条目不可变」（组长 2026-10-02T01:31:16Z 改的口径）。
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
