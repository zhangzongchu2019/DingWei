# R0 · 临时 PG 测试环境（随起随清）

- 派工 `R-NEWPLAT-G2-qwen38maxkhovvob4-20261001084334`（组长 opus50ts，2026-10-01T08:43:34Z）第二件
- 编排件 `runbooks/WORKGROUPS-NEWPLAT-20261001.md` 对 G2 的要求原文：「测试环境（临时 PG＋真 Hub，可随起随清）」
- 代码锚：分支 `qwen38maxkhovvob4/newplat-r0`（基线 `fcd3a3dd`）
- 表清单见 `docs/newplat/R0-table-inventory.md`（第一件）

## 一 · 为什么是「内嵌 PG」而不是别的

本席实测本机条件（逐条核过，不是推断）：

| 路子 | 实测结论 | 证据 |
|---|---|---|
| 本机已装 PG | ❌ 没有 | `which postgres psql pg_ctl initdb` 全空；`/usr/lib/postgresql/*` 不存在 |
| docker 起 PG | ❌ 无权限 | `docker info` → `permission denied ... /var/run/docker.sock` |
| apt 装 PG | ❌ 无 root | `id` → `uid=1006(is3)`，单组；`sudo -n true` → `a password is required` |
| 等 G1 的新主机 PG | ⏳ 未就绪 | 编排件第四节：G1 还在「先用自签证书在 55055 上把 Hub 跑起来」阶段 |
| 连外部 PG（环境变量给 DSN） | ✅ 支持 | 见第二节三级顺序①，沿用 `internal/datad` 既有惯例 |
| **免特权内嵌 PG** | ✅ **实测可用** | 下载 zonky 二进制、以 is3 身份起 PostgreSQL 18.3，无需 root/docker |

⇒ 选内嵌 PG 的理由不是「它好」，而是**它是当时唯一不阻塞的路**：G3 设计件第五节明写
「🔵 在 R0 的 PG 测试环境就绪之前，G3 的 Hub 侧集成用例用内存实现」——他们在等这一格。
外部 DSN 那条路同时保留，G1 的 PG 就绪后可以直接切过去，不必改代码。

网络前置条件（实测可达）：`repo1.maven.org` 返回 200，`proxy.golang.org` 返回 200。
**首次**运行要下载 PG 二进制（约 30 MB）；之后走缓存，实测启动 2.7–3.9 秒。

## 二 · 取库三级顺序（与仓内既有惯例对齐）

`internal/datad` 的既有做法是 `DATAD_TEST_PG_DSN` 门控、未设即 `t.Skip`（见
`internal/datad/pg_integration_test.go:19-21`）。R0 沿用同一形状，只是多了一级兜底：

| 级 | 条件 | 行为 |
|---|---|---|
| ① | `HUB_TEST_PG_DSN` 已设 | 直接用它。本包**不拥有**其生命周期（不启不删），也就**不**断言它有没有删净 |
| ② | 未设，且未禁用内嵌 | 起 `tools/pgtest` 子进程（内嵌 PG），用完停掉并删净，**两处独立验证** |
| ③ | 都不通（无 go 工具链／模块缓存冷且无网／`HUB_TEST_PG_EMBEDDED=0`） | `t.Skip` 并**说明原因**，不静默降级成「假装过了」 |

🔴 `HUB_TEST_PG_DSN` 必须指**独立测试库**，**永不指生产库**——与 `DATAD_TEST_PG_DSN` 同一条纪律。

开关语义按**字面**读（本包首版写反过，见第五节）：

    HUB_TEST_PG_EMBEDDED 未设 / 1 / true / on / yes  ⇒ 允许自行起内嵌 PG
    HUB_TEST_PG_EMBEDDED 0 / false / off / no / 任何认不出的值 ⇒ 禁用（fail-closed）

## 三 · 怎么用

```go
func TestXxx(t *testing.T) {
    pool := pgtest.Pool(t)          // 关池＋停库＋判目录已删,全部自动登记进 t.Cleanup
    ctx := context.Background()
    actx, err := repo.NewAccessCtx("org", "principal")
    // ... pool.Exec / Query / QueryRow / InTx
}
```

只要连接串、自己控制连接形态（例如 datad 那种三池分权）时用 `pgtest.DSN(t)`。

命令行单独起一个库手工玩（不起 go test）：

```
cd tools/pgtest && go build -o pgtest .
./pgtest                 # 打印 DSN=... READY=1 后常驻;关掉它的 stdin 或发 SIGTERM 即停并删净
./pgtest -keep-runtime   # 排障:退出时保留运行目录(CLEANED=kept)
```

可调项：`-port`（0＝自动挑空闲端口）、`-version`、`-cache-root`、`-runtime`、`-start-timeout`。

## 四 · 实测读数（本机 is3，2026-10-01）

    go test ./internal/store/pgtest/ -count=1        ok  13.330s   6 条全 PASS
    go test ./internal/store/pgtest/ -race -count=1  ok  13.403s   竞态干净
    PostgreSQL 18.3 on x86_64-pc-linux-gnu
    synchronous_commit = on · fsync = on            (工具显式钉死,不随库默认漂)
    单进程 smoke(独立于 go test): 启动 3.9s · STOPPED=1 · CLEANED=1 · 运行目录已删 · rc=0

| 用例 | 判什么 | 读数 |
|---|---|---|
| `TestPool_RoundTrip` | 起得来；走的是 R0 真要走的连接路径（`repo.OpenPG`，pgx 驱动）；`version()` 含 PostgreSQL；`synchronous_commit`/`fsync` 均为 on；建表＋写读往返；主键冲突报**错**而非空结果；`InTx` 回滚后读不到 | PASS 2.67s |
| `TestCleanupIsReal_KeepRuntimeNegativeControl` | 🔴 **「随清」的反向对照**：用 `-keep-runtime` 起同一个工具，要求目录**必须还在**且 `CLEANED` **必须不是** 1 | PASS 9.62s |
| `TestReadProtocol_NegativeControls` | 噪音行被忽略而协议行照取；缺 `READY=1` 必须报错；`READY` 超时不来时错误文案要说明是超时 | PASS 0.20s（3 子例） |
| `TestUsable_NegativeControls` | 构建产物复用判据不是恒真：文件缺失→不可用；**源文件比产物新→不可用**（否则会拿旧二进制跑出假绿） | PASS 0.00s（4 断言） |
| `TestEmbeddedDisabled_SemanticsByLiteralReading` | 开关语义按字面读，12 格逐个锁死；判为禁用时**必须给出理由**（否则 Skip 文案缺原因，等于没说明为什么没跑） | PASS（12 子例） |
| `TestPool_SkipsEndToEndWhenEmbeddedDisabled` | 🔴 **端到端对照**：子进程真跑 `-run TestPool_RoundTrip`，要求输出含 `--- SKIP`、且**不许**出现版本日志行（出现即说明库其实被起起来了，Skip 没生效） | PASS 0.83s |

### 为什么「随清」必须有反向对照

`TestPool_RoundTrip` 里「运行目录已删」这条，如果工具把 `CLEANED` 写死成 1、或助手根本不 `stat`，
用例**照样绿**——那就是「判据不起作用但看得见 ✓」，与 `scripts/repo-boundary-gate.sh` 头注里
记过的事故同型。故两处都做了，且互相独立：

1. 工具自报 `CLEANED`／`STOPPED`（它自己的说法）；
2. 助手在 `t.Cleanup` 里**独立 `stat`** 一次运行目录（我们的实测）。

**同一件事不许两处都只自证。** 反向对照再用 `-keep-runtime` 证明 `CLEANED=1` 是真信号而非写死。

## 五 · 开发过程中当场抓到并修掉的三个自身缺陷

如实登记，包括本席自己写错的部分：

| # | 缺陷 | 怎么露出来的 | 修法 |
|---|---|---|---|
| 1 | `readProtocol` 读到 `READY=1` 就收工，**没人读**其后的 `STOPPED`/`CLEANED` ⇒ 自证值恒为空串，而空串不像失败，极易被忽略 | `TestPool_RoundTrip` 报 `CLEANED=""` | 改成流式解析（`newProtocol`），收尾时**先读到 EOF 再 `cmd.Wait()`**（`Wait` 会关管道，顺序反了就截断那两行） |
| 2 | `HUB_TEST_PG_EMBEDDED=0` **没能禁用**，照旧起库——语义写反 | 手工跑 Skip 路径当场发现（不是用例发现的） | 抽成纯函数 `embeddedDisabled()`，白名单外一律 fail-closed；补纯函数表＋端到端 Skip 对照两条用例 |
| 3 | `t.Cleanup` 登记顺序写反 ⇒ 「目录已删」断言抢在停库**之前**跑 | 自查 LIFO 语义 | 登记顺序改为 断言→停库→关池（LIFO 下执行序即 关池→停库→断言），并把后果写进注释 |

第 2 条尤其值得记：它**没有任何用例会变红**。开关控制的是「会不会 Skip」，写反只会让
「按令不许起库」的环境里悄悄起了一个库，或反过来让能跑的环境全跳掉——而跳掉看上去像通过。

## 六 · 与两道仓闸的关系（🔴 这里是本件最需要组长裁定的部分）

### 6.1 依赖棘轮闸：本件**没有**加剧它，但它本来就红

`scripts/dependency-ratchet.sh` 判「直接依赖数只许降不许升」，基线 `specs/gates/dependency-baseline.txt: direct_count=6`。

`embedded-postgres` 会带进 `lib/pq` 与 `xi2/xz`。若并进主 `go.mod`，直接依赖 6→7 即触闸，
而**上调基线属评审面**（须随 spec §4.7.1 ⑤ 的突破论证一并提交，不得静默改）。

⇒ 故 `tools/pgtest` 做成**独立 module**（自带 `go.mod`，理由写在其文件头注）。
实测：主 `go.mod`／`go.sum` 的 `git diff` 为空，直接依赖数一个没动。

🔴 **但必须如实报：这道闸在基线 `fcd3a3dd` 上本来就是红的**，与本件无关。
本席用 `git worktree` 在基线上跑同一道闸复现：

    直接依赖：8（基线 6）  ✗ 依赖数回升 6 → 8
    多出的两条：github.com/minio/minio-go/v7、golang.org/x/sys（go.mod 里未标 indirect）

### 6.2 边界闸：本件新增红 **0**，但暴露一条闸缺陷

`scripts/repo-boundary-gate.sh` 三条棘轮，本件前后对比（基线值取自 `fcd3a3dd` 的 worktree 实测）：

| 棘轮 | 基线值 | 基线实测 | 本件后 | 是否本件引入 |
|---|---|---|---|---|
| 判据一·仓储包外建库句柄 | 4 | **5（已红）** | 5 | ❌ 非本件，基线即红 |
| 债·显式系统上下文调用点 | 12 | 12（绿） | 12 | ✅ 一度被本件顶到 13，已修回 |
| 判据二·裸句柄逃出包外 | 1 | 1（已红） | 1 | ❌ 非本件 |

本件**不出现** `sql.Open`：连接一律走 `repo.OpenPG`（`internal/repo/pool.go:25`），
因为判据一是「仓储包外 `sql.Open` 恒零」，测试夹具也不豁免。

🔴 **闸缺陷（已复现，未擅自修）**：那条「系统上下文」棘轮是**纯正则**计数，
`grep -rnE 'repo\.SystemCtx\(\)' --include=*.go internal`，**连注释一起数**。
基线 12 处里已有 **2 处是注释**（`internal/fanout/spool.go:40`、`internal/datad/pgstore.go:93`）。
本席首版只在**注释里**写了一遍那个字面量，就把 12 顶成 13、多出一条红——
**散文能推动一条声称在量「调用点」的棘轮。**

该闸头注自认过同型问题（SA-12a：旧口径「误计了 2 行注释」），并对 pending 债改用 AST
（`specs/tools/pending-debt` 按效果判）；**但系统上下文这条仍是正则，未一并升级。**
两条判据强度不一样，闸自己的头注也写了「不要一并宣称」。

修法有两种（改闸＝改判据，属评审面，本席不动）：
- (a) 与 pending 债同款，改 AST 只数真调用点；
- (b) 保持正则，但排除注释行（成本极低，但仍是正则，强度弱于 (a)）。

本席当前的绕法：注释里**不写**那个调用字面量，改称「repo 包那个系统上下文构造器」，
并在注释里写明为什么故意不写。这是绕，不是修——已上报。

## 七 · 本件**做不到**什么（不假装）

1. **还没有 PG 版存储实现。** 本件交付的是「临时 PG＋连接路径＋清理自证」，
   用例连的是 `repo.Pool`，**不是** Hub。真正的「真 Hub 跑在 PG 上」要等
   `store.Repository` 的 PG 实现（见表清单第九节：198 个去重方法），那时才有 Hub 级用例。
   ⇒ 编排件那句「临时 PG＋真 Hub」，本件只做到了前半句，**如实报**。
2. **不覆盖 board／fileservice／fanout 三个独立进程的库**（表清单第三节）。
   它们的 PG 化各自要单独起、单独测；本件的助手可复用，但那三格尚未接。
3. **端口选取有理论上的抢占窗口**：`pickFreePort` 拿到端口后要先关监听再交给 PG。
   故意不做重试——被抢时 PG 启动会直接失败并报出端口号，比静默重试更容易定位。
4. **首次运行需要网络**（下载 PG 二进制与 go 依赖）。无网且模块缓存冷时走③ `t.Skip`，
   不会失败，但也就**没有覆盖**；CI 若要真覆盖，需预热缓存或给 `HUB_TEST_PG_DSN`。
5. **二进制缓存目录跨运行复用**（默认 `~/.embedded-postgres-go`）。
   清的是**数据目录**（每次全新、退出即删），不清下载/解压缓存——那是构建缓存，不是测试状态。
   要一并清用 `-cache-root` 指到临时目录。

## 八 · 交付面清单

| 文件 | 性质 | 说明 |
|---|---|---|
| `tools/pgtest/main.go` | 新增（独立 module） | 内嵌 PG 起停器；stdout 只走协议行，PG 日志全导 stderr；自己删运行目录并自证 |
| `tools/pgtest/go.mod` | 新增 | 独立 module，头注写明为何不并进主 `go.mod` |
| `tools/pgtest/go.sum` | 新增 | 同上 |
| `internal/store/pgtest/pgtest.go` | 新增 | 主模块测试助手，**只用 stdlib＋`internal/repo`**，不给主 `go.mod` 添依赖 |
| `internal/store/pgtest/pgtest_test.go` | 新增 | 6 条用例，含 2 条反向对照 |
| `.gitignore` | 改 1 处 | 忽略本地构建产物 `/tools/pgtest/pgtest`，源码照常入库 |

未改：任何 schema、任何判据、`go.mod`／`go.sum`、`specs/gates/*` 基线、preprod。
