# coder56solzzc 当前恢复件

> 编排与组长交接以 `runbooks/HANDOFF-LEAD-20261002.md`（板锚 `bfcdcc8d`）为准；本件只记录本席可恢复现场。

更新时间：2026-10-02T01:24:59Z

## 身份与边界

- 会话：`zzc-coder56solzzc-2642`
- 工作组：G2 存储组
- 在办范围：`internal/fanout` spool（5 表）先行；随后 `internal/mcs/board`（11 表）。
- 旧平台事项已全部撤销；不得继续 C 交叉复验、S-PYTEST-SILENT-TRUNCATION 或其它旧平台修复。
- 不触生产；新平台开发只在个人分支进行，经独立复验和组长集成。

## 当前 Git 现场

- 仓库：工蜂 `wego-ai/dingwei`
- 工作区：`/data/ai-dev-data/dingwei-sp-group2`
- 分支：`coder56solzzc/newplat-g2-fanout-spool`
- 当前 tip：`5fd906b6b20effb557877542f031715561fc9336`
- 基线：`newplat/main@5fd906b6`
- 本席产品提交：尚无。
- 当前工作树：保存本件前干净。
- 后续接集成线纪律：先 `merge` 最新 `newplat/main`，不得 rebase；不得再合并其他成员分支。

## fanout 已冻结实施口径

1. `OpenSpool` 注入 `repo.Pool`，不接收 PG DSN。
2. 删除 `Config.SpoolPath`，不留 SQLite 兼容开关。
3. 仅用 PostgreSQL；`INSERT OR IGNORE` 改为 `ON CONFLICT DO NOTHING`，占位符改为 `$n`。
4. 测试夹具统一使用 `internal/store/pgtest`。
5. “提交即落盘”须有机械判据，并有反向对照：把事务提交挪到返回值之后时，用例必须显式红。
6. 新增 `repo.SystemCtx()` 调用必须逐条列位置与理由，并在同一提交更新 `specs/gates/boundary-baseline.txt`。
7. 涉及顺序、时钟、并发或随机的交付，必须附 `-count=50 -shuffle=on` 读数。
8. 复验方：`opus50tc2k`；承办不得自验收。

## 下一步

1. 先核最新远端 `newplat/main`；如前进，以 merge 方式接入。
2. 写 fanout 切片 spec 草稿，判据编号与实现同步。
3. 将 fanout 初始化改为注入 `repo.Pool`，PG DDL 一次初始化 5 张表。
4. 改全部 SQL 方言、调用点和 `cmd/feishu-fanout` 的 PG 池装配；删除路径型配置与磁盘空间预检。
5. 把 custody、inbound、admin 和命令级测试迁到 `pgtest`，补同步提交正例与“返回先于提交”反向判据。
6. 跑专项、多轮随机、全仓相关闸和构建；记录完整读数后提交、推送并交 `opus50tc2k` 复验。

## 等待与阻塞

- 当前不等待他人，无外部阻塞。
- 主要风险是 PG 测试夹具的库隔离：并行或多轮测试必须避免固定表名互相污染。
- 原定 2026-10-01T15:30:00Z 的 fanout 首件未交付；不得在恢复后误报为已完成。

## 未交读数

- fanout PG 专项：未跑。
- “提交即落盘”正例及反向对照：未实现、未跑。
- `go test -count=50 -shuffle=on`：未跑。
- build、repo boundary gate：未跑。

## 本席纪律

- 对状态如实报告；只读盘点不算编码，单跑不算多轮稳定性证据。
- 先取远端锚，再开发和交付；报告分支、完整 tip、改动面、判据编号、用例读数和未覆盖项。
- 凭据、token、cookie、密码和连接秘密不进入 Git、日志或消息正文。
- 私有仓提交身份固定为 `zhangzongchu <zzc@zhangzongchu.com>`，不添加工具署名或 co-author。
- 共享主机停止测试进程只按记录 PID 精确停止，不用 `pkill -f` 或 `killall`。
- 跑 sessionhelper 测试前检查固定测试残留路径；存在时只改名备份移开，不读取、不贴内容。
