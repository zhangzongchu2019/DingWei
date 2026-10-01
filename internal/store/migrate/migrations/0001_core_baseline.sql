-- 0001 · core 基线（G2 迁移账本首条）
-- 冻结时刻 2026-10-01，底座 dd69f88a（新平台 Hub 的 PG 装配那一版）。
--
-- 本条内容 ＝ 那一刻 core 侧【已经在 PG 上】的全部表：只有 audit_log。
-- 🔴 不要把 SQLite 侧 internal/store/migrations/0001_init.sql 的 61 张表读成「都该在这里」——
--    Hub 主库目前仍是 SQLite，只有身份域（0100）搬到了 PG；core 其余表随 G2「PG 骨架」那一批
--    以【新增条目】进账本（号段 0002-0099），不是改本条。本条一经在长命库上记账即不可变。
--
-- audit_log 的 PG 形状为什么与 SQLite 侧【不同形】（口径页 §三；组长裁定 ruling-2026-10-01T152239Z 四）：
--   SQLite 0001_init.sql:205-213 是 `id TEXT PRIMARY KEY`（无默认）＋ `ts TEXT NOT NULL`。
--   照直译到 PG，身份域的写入路径（internal/identity/pgstore.go 的 AppendAudit：只给
--   actor/action/target/after_json/ts，不给 id，ts 传 time.Time）会两头撞墙，本席探针实跑过：
--     · 不给 id ⇒ 23502 null value in column "id" violates not-null constraint
--     · ts 传 time.Time 给 text 列 ⇒ 无法编码（cannot convert time.Time to text）
--   而调用方 internal/identity/service.go 把审计错误丢弃 ⇒ 结果是【审计静默全丢】，不是启动报错。
--   ⇒ PG 侧：id 带默认（bigserial），ts 用 timestamptz。
--   🔴 两库不共享数据，故「不同形」不构成兼容问题；将来要把旧 SQLite 数据导进 PG，走 G2 的导入件，
--      届时 id 的映射（text → bigint）在导入件里解决，不回头改本条。
--   🔵 action 保持 NOT NULL（与 SQLite 侧一致）：身份域恒传字符串，空串合法、NULL 不合法。
--      测试夹具里的那张最小 audit_log（internal/identity/identitytest/pgschema.go）action 可空，
--      比本条宽；宽夹具测不出「action 缺失」这类写入，故以本条为准。
--
-- 🔵 顺序约束（0100 的头注也写着同一句）：身份域【复用】这张 audit_log、自己不建，
--    故本条必须先于 0100_identity_baseline.sql 存在。号段顺序（0001 < 0100）已保证这一点；
--    改号前先想清楚这条依赖。

CREATE TABLE IF NOT EXISTS audit_log (
  id          bigserial   PRIMARY KEY,
  actor       text,
  action      text        NOT NULL,
  target      text,
  before_json text,
  after_json  text,
  ts          timestamptz NOT NULL
);
