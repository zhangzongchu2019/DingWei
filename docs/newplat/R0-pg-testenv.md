# R0 · 临时 PG 测试环境（随起随清）

- 派工 `R-NEWPLAT-G2-qwen38maxkhovvob4-20261001084334`（组长 opus50ts，2026-10-01T08:43:34Z）第二件
- 编排件 `runbooks/WORKGROUPS-NEWPLAT-20261001.md` 对 G2 的要求原文：「测试环境（临时 PG＋真 Hub，可随起随清）」
- 代码锚：分支 `qwen38maxkhovvob4/newplat-r0`（基线 `fcd3a3dd`）
- 表清单见 `docs/newplat/R0-table-inventory.md`（第一件）
- 复验：板 `93a10885`（opus50tc2k，判「不通过，一处须修」）⇒ 本版按其 🔴①／🟡②／🟡③ 逐条修，
  另加自查发现的两条（僵尸残留、二进制重复解压），见**第五之二节**

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
| ③ | 都不通（无 go 工具链／模块缓存冷且无网／`HUB_TEST_PG_EMBEDDED=0`） | `t.Skip` 并**说明原因**，不静默降级成「假装过了」。🔴 只有**环境本就缺失**才走这一格；「环境在却起不来」另算 —— **默认判 Fail**，见下面的 `HUB_TEST_PG_SKIP_OK` 与五之三 |

🔴 `HUB_TEST_PG_DSN` 必须指**独立测试库**，**永不指生产库**——与 `DATAD_TEST_PG_DSN` 同一条纪律。

开关语义按**字面**读（本包首版写反过，见第五节；`STRICT` 那一格的默认值后来被翻转，见**五之三**）：

    HUB_TEST_PG_EMBEDDED 未设 / 1 / true / on / yes  ⇒ 允许自行起内嵌 PG
    HUB_TEST_PG_EMBEDDED 0 / false / off / no / 任何认不出的值 ⇒ 禁用（fail-closed）

    HUB_TEST_PG_SKIP_OK  未设 / 0 / false / off / no / 否  ⇒ **默认**：「环境在却起不来」判 Fail
    HUB_TEST_PG_SKIP_OK  1 / true / on / yes / 是          ⇒ 放行：这类失败降级成 Skip（文案点明是谁放行的）
    HUB_TEST_PG_SKIP_OK  任何认不出的值                    ⇒ 用例直接 Fail（打错的开关不许静默生效）

    HUB_TEST_PG_STRICT   未设                              ⇒ 无影响（判据只由上面那个开关决定）
    HUB_TEST_PG_STRICT   1 / true / on / yes / 是          ⇒ 无影响，与默认一致（过渡期读数原样认下）
    HUB_TEST_PG_STRICT   0 / false / off / no / 否         ⇒ **Fail**：旧「关」不再表示「允许 Skip」，报错并指名改用 SKIP_OK
    HUB_TEST_PG_STRICT   任何认不出的值                    ⇒ **Fail**：本开关已随默认翻转废弃

「环境本就缺失」与「环境在却起不来」的分界（判据只有 `unavailable()` 一个出口，免得漏分类）：

| 情况 | 归类 | 默认 | `HUB_TEST_PG_SKIP_OK=1` |
|---|---|---|---|
| `HUB_TEST_PG_EMBEDDED=0`（按令不起库） | 缺失 | Skip | Skip |
| 找不到 `go` 工具链 | 缺失 | Skip | Skip |
| `PGTEST_BIN` 指的文件不存在 | 坏了 | **Fail** | Skip |
| 工具编不出来／仓根定位不到／源目录不像工具源 | 坏了 | **Fail** | Skip |
| 工具起了但在 `READY=1` 前退出、起库超时、协议行缺 `DSN` | 坏了 | **Fail** | Skip |

🔴 **默认即严格**：CI 与交付读数**不需要**再置任何开关。过渡期（翻转合入前）各组被要求带的
`HUB_TEST_PG_STRICT=1` 仍可原样带着，效果与默认相同。

🔴 `HUB_TEST_PG_SKIP_OK=1` 只用于**本地**确实没有 PG、又只想跑非 PG 用例的场合。它会把
「环境在却起不来」放行成 Skip，所以**不得**写进 CI 或交付读数的环境里——那等于把这道判据关掉，
而整包仍然报 `ok`，正是复验 🟡② 撞上的形状。

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

可调项：`-port`（0＝自动挑空闲端口）、`-version`、`-cache-root`、`-runtime`、`-start-timeout`、
`-keep-runtime`（保留**运行目录与日志目录**两者，`CLEANED=kept`）。

协议行（stdout 只有这些；PG 与 pg_ctl 的日志全在 stderr）：

    PORT RUNTIME LOGDIR                       ← 在 Start() 【之前】就报：失败路径上助手也知道该核哪两个目录
    BINARIES CACHE DSN START_SECONDS READY=1
    STOPPED=1/0  CLEANED=1/kept/0  RUNTIME_STATE  LOGDIR_STATE  PGLOG_REDIRECTED=1/0  REAPED=n

`CLEANED` 是**合成值**：运行目录与日志目录各自判（`RUNTIME_STATE`／`LOGDIR_STATE`），
任一个「该删没删」⇒ `0`；有任一个按配置该留 ⇒ `kept`；两个都删净 ⇒ `1`。
拆开的原因：只有一个布尔时，「运行目录删了、日志目录漏了」会合成出同一个「删了」。

助手侧还有一道**不采信自报**的独立核对（`residue.go`）：起库前后各拍一次快照，只报**新增**残留——
系统临时目录里新出现的 `embedded_postgres_log*`、进程表里新出现的 `postgres` 僵尸。
按**增量**判而非绝对值：跑测试的容器 PID 1 是 `sleep infinity`，不收孤儿，
修复前攒下的 25 个僵尸谁都清不掉；按绝对值判会让历史残留把之后每一次跑都判红，
而判据一红就没人看了——那比没有判据更糟。代价如实写：增量判据看不见**本次之前**就存在的残留。

## 四 · 实测读数（本机 is3，2026-10-01；本版＝复验 `93a10885` 后的修订版 ＋ 默认翻转（五之三）后重测）

    go test ./internal/store/pgtest/ -count=1               ok  10.5s     10 条全 PASS
    go test ./internal/store/pgtest/ -race -count=1         ok  11.174s   竞态干净

    🔁 默认翻转（五之三）后重测,2026-10-01T13:2xZ,基 newplat/main 97c69c2e:
       go test ./internal/store/pgtest/ -count=1            ok  11.907s   10 条全 PASS、**0 SKIP**
       go test ./internal/store/pgtest/ -race -count=1      ok  12.816s   竞态干净
       🔴 反向对照(复验方要跑的那一格,本席先自证):把 skipAllowed 的默认改回「允许 Skip」⇒ **4 格立刻变红**
          · TestSkipAllowed_SemanticsByLiteralReading/两个都不设
          · TestSkipAllowed_SemanticsByLiteralReading/STRICT=1
          · TestSkipOKDefault_EndToEnd/默认_⇒_FAIL,不许盖成_SKIP
          · TestSkipOKDefault_EndToEnd/过渡期读数_STRICT=1_⇒_仍_FAIL
          改回原样后同一批用例复绿(ok 4.497s)。
    go test ./internal/store/pgtest/ -count=50 -shuffle=on  ok  495.603s  0 FAIL（板 a9dbf12e 的新规矩）
                                                            seed 1790852949886354352
                                                            11:09:08Z → 11:17:25Z
    PostgreSQL 18.3 on x86_64-pc-linux-gnu
    synchronous_commit = on · fsync = on            (工具显式钉死,不随库默认漂)
    单进程 smoke(独立于 go test): 启动 2.2s · STOPPED=1 · CLEANED=1 · RUNTIME_STATE=cleaned
                                  · LOGDIR_STATE=cleaned · PGLOG_REDIRECTED=1 · REAPED=1 · rc=0

    「随起随清」在 -count=50 那一轮（共起停 100 次 PG）前后的独立读数:
      系统临时目录 embedded_postgres_log*   26 → 26   零增量（🟡③ 修前每起一次漏一个）
      postgres 僵尸                        27 → 27   零增量（🔵④ 修前每起一次留一个）
      /tmp/dingwei-pgtest*                 跑完 0 个
      活 postgres 进程                      跑完 0 个
    🔴 那两个基线数不是 0：是修复前攒下的历史残留，本席这台上清不掉（僵尸只能由父进程收，
    而 PID 1 是 sleep infinity）。按绝对值判会让它们把之后每一次跑都判红，故按增量判 —— 见第七节第 8 条。

    🔁 翻转后重测时的环境残留绝对值:`embedded_postgres_log*` **45**、`postgres` 僵尸 **46**。
       比上面那轮的 26／27 多出的 **19／19 不是本件漏的**,已归因:那 19 个日志的 mtime 全落在
       2026-10-01T12:39:26Z–12:40:20Z、每 3 秒一个,是本席在 `kimi30kqtuxerh/g4-m0-m1`(tip `90bc3c02`)
       那棵树上跑 `go test ./internal/hosted/ -count=1 -v`(24 条用例)时留下的 —— 那棵树上的 pgtest 还是
       **旧版**(`c7f4ecda` 的 5 文件版),🟡③(漏日志)与 🔵④(漏僵尸)两个缺陷它都还带着。
       本版重跑一轮(起停 2 次真 PG):logs 45→45、zombies 46→46,**增量 0**;`/tmp/dingwei-pgtest*` 跑完 0 个。

    两道仓闸（与复验核过的读数逐行一致 ⇒ 本版新增红 0）:
      dependency-ratchet   直接依赖 8（基线 6）—— 既有红，已单开给 togpt56terrats
      repo-boundary-gate   判据一 5（基线 4）既有红 · SystemCtx 12＝12 · 判据二 1＝1
                          🔁 翻转那一提交之后重跑同读数（5／55／12／1）⇒ 本提交新增红 **0**
      git diff c7f4ecda 起未碰任何 go.mod／go.sum（本包的依赖仍全关在 tools/pgtest 那个独立 module 里）

| 用例 | 判什么 | 读数 |
|---|---|---|
| `TestPool_RoundTrip` | 起得来；走的是 R0 真要走的连接路径（`repo.OpenPG`，pgx 驱动）；`version()` 含 PostgreSQL；`synchronous_commit`/`fsync` 均为 on；建表＋写读往返；主键冲突报**错**而非空结果；`InTx` 回滚后读不到 | PASS 3.70s |
| `TestCleanupIsReal_KeepRuntimeNegativeControl` | 🔴 **「随清」的反向对照**：用 `-keep-runtime` 起同一个工具，要求**两个**目录都**必须还在**且 `CLEANED` **必须不是** 1；🟡③ 的**正证**：日志目录里真的有 `embedded_postgres_log*`（证明重定向接住的就是库那个文件）；协议行齐全（`RUNTIME_STATE`／`LOGDIR_STATE`／`PGLOG_REDIRECTED`／`REAPED`） | PASS 2.85s（改前 8.42s，见 🔵⑤） |
| `TestReadProtocol_NegativeControls` | 噪音行被忽略而协议行照取；缺 `READY=1` 必须报错；`READY` 超时不来时错误文案要说明是超时 | PASS 0.20s（3 子例） |
| `TestUsable_NegativeControls` | 构建产物复用判据不是恒真：文件缺失→不可用；**源文件比产物新→不可用**（否则会拿旧二进制跑出假绿）。🔴 修 🔴① 之后它**只管树内预构建产物**；共用缓存目录一律走内容哈希定名，因为「产物比源新」根本不能证明「产物是这份源编的」 | PASS 0.00s（4 断言） |
| `TestEmbeddedDisabled_SemanticsByLiteralReading` | 开关语义按字面读，12 格逐个锁死；判为禁用时**必须给出理由**（否则 Skip 文案缺原因，等于没说明为什么没跑） | PASS（12 子例） |
| `TestPool_SkipsEndToEndWhenEmbeddedDisabled` | 🔴 **端到端对照**：子进程真跑 `-run TestPool_RoundTrip`，要求输出含 `--- SKIP`、且**不许**出现版本日志行（出现即说明库其实被起起来了，Skip 没生效） | PASS 0.83s |
| `TestToolBinaryCache_KeyedOnContentNotMtime` | 🔴 **🔴① 的反向对照**：两份内容不同、mtime 相同的源 ⇒ 产物名**必须不同**；内容相同 ⇒ 名相同（缓存没被修坏）；只改 mtime ⇒ 名不变（证明时间戳不再参与判定）；多一个源文件 ⇒ 名必须变（挡住「写死只哈希两个文件」）；内容在两文件间挪一挪 ⇒ 名必须变（挡纯拼接歧义）；构建产物自己不算进哈希（否则缓存永不命中） | PASS 0.01s（6 子例） |
| `TestSkipAllowed_SemanticsByLiteralReading` | 🟡② ＋ 五之三翻转的开关判据：19 格逐个锁死。含**认不出的值必须报错**（打错的 `SKIP_OK=ture` 静默生效＝判据死了但看着全绿）；含翻转的两条不变量——**都不设必须「不允许 Skip」**（撤翻转本格立刻变红）、`STRICT` 的任何取值都不得让判据返回「允许」（旧「关」＝报错，不静默忽略，且即便同时置了 `SKIP_OK=1` 也报错） | PASS（19 子例） |
| `TestSkipOKDefault_EndToEnd` | 🔴 **翻转的端到端对照**：拿「起了就死、连一行协议都不打」的假工具当故障环境（形状同复验 M5），在子进程里真跑 `TestPool_RoundTrip` ⇒ 默认必须 `--- FAIL`、`SKIP_OK=1` 必须 `--- SKIP` 且文案点明是谁放行的、`SKIP_OK=ture` 必须 `--- FAIL`、`STRICT=1` 必须 `--- FAIL` 且**不得**冒废弃报错、`STRICT=0` 必须 `--- FAIL` 且文案指名改用 `SKIP_OK` | PASS 4.62s（5 子例） |
| `TestResidueWatch_NegativeControls` | 🔴 **独立核对不是摆设**（🟡③／🔵④）：故意在系统临时目录造一个同前缀文件 ⇒ 必须被抓到，收掉后必须**不再**报；**真造一个** `comm=postgres` 的僵尸（把 `true` 复制成名叫 `postgres` 的可执行文件、起了不 `Wait`）⇒ 必须抓到，收尸后必须不再报；另判 `/proc/pid/stat` 解析在 comm 含空格与含右括号时不取错列 | PASS 0.15s（3 子例） |

### 为什么「随清」必须有反向对照

`TestPool_RoundTrip` 里「运行目录已删」这条，如果工具把 `CLEANED` 写死成 1、或助手根本不 `stat`，
用例**照样绿**——那就是「判据不起作用但看得见 ✓」，与 `scripts/repo-boundary-gate.sh` 头注里
记过的事故同型。故两处都做了，且互相独立：

1. 工具自报 `CLEANED`／`STOPPED`／`RUNTIME_STATE`／`LOGDIR_STATE`／`PGLOG_REDIRECTED`／`REAPED`（它自己的说法）；
2. 助手在 `t.Cleanup` 里**独立 `stat`** 两个目录（运行目录与日志目录）；
3. 助手在起库**前后各拍一次快照**、比**增量**（`residue.go`）：系统临时目录里新出现的
   `embedded_postgres_log*`、进程表里新出现的 `postgres` 僵尸。这一道**不依赖工具报的任何路径** ——
   工具撒不撒谎、报不报 `LOGDIR`，它都照样看得见。复验 🟡③ 那处泄漏之所以两道自证都没发现，
   正是因为漏的东西不在它们核的那个路径里。

**同一件事不许两处都只自证。** 反向对照再用 `-keep-runtime` 证明 `CLEANED=1` 是真信号而非写死；
并用「**故意造残留**」证明第 3 道真会红（`TestResidueWatch_NegativeControls`）——
判据本身恒不报时，表现恰恰像通过，所以这道对照不能省。

## 五 · 开发过程中当场抓到并修掉的三个自身缺陷

如实登记，包括本席自己写错的部分：

| # | 缺陷 | 怎么露出来的 | 修法 |
|---|---|---|---|
| 1 | `readProtocol` 读到 `READY=1` 就收工，**没人读**其后的 `STOPPED`/`CLEANED` ⇒ 自证值恒为空串，而空串不像失败，极易被忽略 | `TestPool_RoundTrip` 报 `CLEANED=""` | 改成流式解析（`newProtocol`），收尾时**先读到 EOF 再 `cmd.Wait()`**（`Wait` 会关管道，顺序反了就截断那两行） |
| 2 | `HUB_TEST_PG_EMBEDDED=0` **没能禁用**，照旧起库——语义写反 | 手工跑 Skip 路径当场发现（不是用例发现的） | 抽成纯函数 `embeddedDisabled()`，白名单外一律 fail-closed；补纯函数表＋端到端 Skip 对照两条用例 |
| 3 | `t.Cleanup` 登记顺序写反 ⇒ 「目录已删」断言抢在停库**之前**跑 | 自查 LIFO 语义 | 登记顺序改为 断言→停库→关池（LIFO 下执行序即 关池→停库→断言），并把后果写进注释 |

第 2 条尤其值得记：它**没有任何用例会变红**。开关控制的是「会不会 Skip」，写反只会让
「按令不许起库」的环境里悄悄起了一个库，或反过来让能跑的环境全跳掉——而跳掉看上去像通过。

## 五之二 · 复验（板 `93a10885`）后修的三条，与自查新发现的两条

复验判「不通过，一处须修」。三条照修，另两条是本席在改的过程中自己撞出来的，一并如实登记。
每一条都配了**反向对照**——判据本身要是恒不报，表现恰恰像通过，所以必须证明它真会红。

| # | 来源 | 缺陷 | 修法 | 反向对照（证明判据会红） |
|---|---|---|---|---|
| 🔴① | 复验 | 构建缓存 `~/.cache/dingwei-pgtest/pgtest` **全机共用**，却只按「源不比产物新」判复用 ⇒ 在 A 工作树里拿到 B 工作树的源码编出的二进制。复验实测 5 连红（**假红**）；顺序反过来就是**假绿** | 缓存产物按**源内容哈希**定名 `pgtest-<sha256(工具源)[:16]>`；源文件按目录**枚举**（不写死 `main.go`＋`go.mod`，免得工具拆文件后又漏哈希）；先编到临时名再 `rename`（同目录原子，并发不会读到半成品；撞 `ETXTBSY` 时同哈希⇒同内容，用现成那份）。树内预构建 `tools/pgtest/pgtest` 是每工作树各一份、无跨树污染，仍按 mtime 判 | `TestToolBinaryCache_KeyedOnContentNotMtime` 6 子例：**两份内容不同、mtime 相同 ⇒ 产物名必须不同**；内容相同 ⇒ 名相同（缓存没被修坏）；只改 mtime ⇒ 名不变（证明时间戳不再参与）；多一个源文件 ⇒ 名必须变；内容在两文件间挪一挪 ⇒ 名必须变（挡纯拼接歧义）；构建产物自己不算进哈希 |
| 🟡② | 复验 | 起库失败一律 `t.Skip`，整包仍报 `ok` ⇒ 将来挂在 `Pool` 上的 G3 PG 契约会静默全跳；且 `KeepRuntime` 反证的内联 reader 在 EOF 时不发信号 ⇒ 工具早退就卡满 6 分钟 `startTimeout`（复验 M5 实测卡死） | 加 `HUB_TEST_PG_STRICT`：严格模式下「环境在却起不来」改 `Fail`，「环境缺失」仍 `Skip`（分界表见第二节）；认不出的开关值直接 Fail。反证用例改用生产那套 `newProtocol`（`await` 在 EOF 时会返回错误 ⇒ 不再卡死；`kv` 由互斥锁看着 ⇒ 顺带消掉一处数据竞争） | `TestStrictMode_SemanticsByLiteralReading`（🔴 已随五之三的翻转改名为 `TestSkipAllowed_SemanticsByLiteralReading`，格数与不变量都变了）14 格（含 `ture`／`2` 必须报错）＋ `TestStrictMode_EndToEnd`（🔴 已改名为 `TestSkipOKDefault_EndToEnd`，且「默认」那一格由 `--- SKIP` 翻成 `--- FAIL`）3 格：拿「起了就死」的假工具当故障环境，默认必须 `--- SKIP`、置 1 必须 `--- FAIL`、认不出的值必须 `--- FAIL` 且文案点明是哪个开关 |
| 🟡③ | 复验 | 每起一次 PG 在系统临时目录漏一个 `embedded_postgres_log*`（约 2.7KB）。来源是 embedded-postgres v1.34.0 的 `logging.go:18`（`os.CreateTemp("",…)`，认 `TMPDIR`，用完不删）；助手只 stat 运行目录 ⇒ 两道自证都看不见 | 工具建**独立日志目录**，起库前把 `TMPDIR` 指过去，退出时两个目录都删，`CLEANED` 以**两个都没了**为准，并多发 `LOGDIR=`／`PGLOG_REDIRECTED=`。🔴 日志目录**不能**放运行目录里：那次 `CreateTemp` 发生在 `embedded_postgres.go:95` 的 `RemoveAll(runtimePath)` **之前**，放进去会被连锅端（句柄还开着、文件已 unlink ⇒「看着不漏了」而日志也没了）。助手侧对 `RUNTIME` 与 `LOGDIR` 都 stat，再加 `residue.go` 的独立快照 | `TestCleanupIsReal_KeepRuntimeNegativeControl` 加了**正证**：`-keep-runtime` 下日志目录必须在，且里面**真的有** `embedded_postgres_log*`（证明重定向接住的是库那个文件，不是假想中的文件）；`TestResidueWatch_NegativeControls` 故意在系统临时目录造一个同前缀文件 ⇒ 必须被抓到，收掉后必须**不再**报（一直红的判据等于没有） |
| 🔵④ | **自查** | 每起一次库在进程表留一个 `postgres` **僵尸**：`pg_ctl start` 起了 postmaster 就退出 ⇒ postmaster 是孤儿，过继给 PID 1，而容器 PID 1 是 `sleep infinity`，**不收尸**。本席实测攒了 25 个且谁都清不掉。后果不只是难看：复查残留的人 `pgrep postgres` 会数到僵尸，**读成「库还活着」** | 工具起库前设 `PR_SET_CHILD_SUBREAPER`（孤儿改为过继给它自己），`Stop()` 返回**之后**再 `Wait4(-1, WNOHANG)` 收尸（顺序很要紧：中途插手会抢走库自己 `cmd.Run()` 的退出状态，让它报 `wait: no child processes` 这种假错）。Linux 专有 ⇒ 拆 `reap_linux.go`／`reap_other.go`，非 Linux 为 no-op | `TestResidueWatch_NegativeControls` 里**真造一个** `comm=postgres` 的僵尸（把 `true` 复制成名叫 `postgres` 的可执行文件、起了不 `Wait`）⇒ 探测器必须抓到；收尸后必须不再报。另判 `/proc/<pid>/stat` 解析：comm 含空格（本容器 PID 1 就叫 `sleep infinity`）与含右括号时不能取错列 |
| 🔵⑤ | **自查** | 给了 `-runtime` 就把二进制与下载缓存也塞进运行目录，而 `Start()` 上来 `RemoveAll(runtimePath)` ⇒ **每跑一次都重新解压一遍 PG**。实测那条反证用例 8.4s，默认路径只要 3.0s；`-count=50` 就是 50 次解压（每次约 250MB 写）。另有两处自相矛盾：`-cache-root` 与 `-runtime` 同给时 `-cache-root` 被吃掉；取不到 `HOME` 时 `binaries` 会变成相对路径 `extracted` | `-runtime` 只管数据目录与「留着给人看的现场」；`cache` 由 `-cache-root` 决定（缺省 `~/.embedded-postgres-go`），`binaries` 在 `cache` 下。反证用例实测 8.42s → 2.85s | 该反证用例本身：目录该在的还得在、`CLEANED` 该是 `kept` 还得是 `kept`（判据一字未动，只是不再重复解压） |

**🔴① 的跨工作树实证**（不是推演，是本席当场跑出来的）：

1. 开一个 `git worktree`（检出的是**已提交**的旧版助手），在其 `tools/pgtest/main.go` 末尾加一行；
2. 它按旧逻辑构建 ⇒ 共用缓存里那份无哈希名的 `pgtest` 被**它编的产物覆盖**（04:01:18，8826469 字节）；
3. 而主树 `tools/pgtest/main.go` 的改时是 03:42:07 ⇒ 按旧的 `usable()`（源不比产物新）**判为可复用** ⇒
   主树会去跑那棵树的二进制。这就是复验说的假红／假绿，两个时间戳都在上面。
4. 换成修好的助手重跑同一实验：共用缓存里并存 `pgtest-67eba24ddd3a4bd4`（主树）与
   `pgtest-a31bfe1de530cf55`（对照树，源多一行），**互不覆盖**；主树第二次跑不再构建（产物改时未变）。

🔴 上表 🟡② 那一格的**默认值后来被翻转**（见下一节）：表中「默认必须 `--- SKIP`、置 1 必须 `--- FAIL`」
是当时的判据，现已反过来 —— 默认必须 `--- FAIL`，只有显式 `HUB_TEST_PG_SKIP_OK=1` 才 `--- SKIP`。

## 五之三 · 默认翻转：`STRICT` 换成 `SKIP_OK`（裁定 `ruling-2026-10-01T130835Z`）

复验第二轮（板 `bd6942a3`）判 R0 通过，但点出 🟡② 的**默认值与组长派法相反**：派的是「默认判失败，
只有显式设了跳过开关才允许跳过」，实现的是「默认跳过，显式 `HUB_TEST_PG_STRICT=1` 才判失败」。
复验方给了二选一（甲＝翻转默认；乙＝保留现状但把「CI 与所有交付读数一律带 `STRICT=1`」写进编排件第〇节），
组长**取甲**。

为什么甲更好（不只是「照派法办」）：乙把判据的正确性寄托在**每个调用方都记得置一个环境变量**上——
漏置的表现是**全绿**，而漏置的人恰好是最不知道该置的人（新接 `Pool` 的域）。甲把默认设成安全侧，
要放宽必须**显式**、且放宽这件事会写在 Skip 文案里（日志上看得出这次绿是「真过」还是「被放过」）。

**改了什么**

| 面 | 翻转前 | 翻转后 |
|---|---|---|
| 默认（两个开关都不设） | 「环境在却起不来」⇒ Skip | ⇒ **Fail** |
| 允许 Skip 的开关 | `HUB_TEST_PG_STRICT` 未设／置「关」 | **只有** `HUB_TEST_PG_SKIP_OK` 置「开」 |
| `HUB_TEST_PG_STRICT=1` | 判 Fail | 无影响（与默认一致）；**原样认下**，过渡期读数不报错 |
| `HUB_TEST_PG_STRICT=0` | 允许 Skip | **报错并指名改用 `SKIP_OK`**（不静默忽略，见下） |
| 认不出的开关值 | Fail | Fail（不变） |
| 「环境本就缺失」（无 go 工具链、按令禁用内嵌 PG） | Skip | Skip（不变，与 `SKIP_OK` 无关） |

🔴 **为什么遗留的 `STRICT=0` 选择报错而不是忽略**：旧语义里「关」＝允许 Skip。翻转后若静默忽略它，
一处 CI 上遗留的 `STRICT=0` 就会让人以为「这里允许跳过」，而实际判据已翻——那是开关读错里最坏的一种
（看着绿、判据不明）。报错文案直接给出改口办法（置 `SKIP_OK=1`，或把旧变量从环境里去掉）。

**判据（反向对照）**

- `TestSkipAllowed_SemanticsByLiteralReading` 19 格：两个开关的每个字面值各判一遍，含三条翻转不变量——
  都不设必须**不允许** Skip（撤翻转本格立刻变）、`STRICT` 的任何取值都不得让判据返回「允许」、
  `STRICT=0` 即使同时置了 `SKIP_OK=1` 也必须报错（不静默）；认不出的值（`ture`／`2`）必须报错且**不得**
  同时返回「允许」。
- `TestSkipOKDefault_EndToEnd` 5 格：拿「起了就死」的假工具当故障环境（`PGTEST_BIN` 指过去），
  在子进程里真跑 `TestPool_RoundTrip` —— 默认必须 `--- FAIL`、`SKIP_OK=1` 必须 `--- SKIP` 且文案点明是谁放行的、
  `SKIP_OK=ture` 必须 `--- FAIL`、`STRICT=1` 必须 `--- FAIL` 且**不得**冒出废弃报错、
  `STRICT=0` 必须 `--- FAIL` 且文案指名 `SKIP_OK`。
- 🔴 复验方的反向对照（裁定明写的那一格）：**撤掉翻转**（把默认改回宽松）⇒ 默认模式下起库失败的用例
  必须由 `--- FAIL` 变 `--- SKIP`，上面两个用例的第一格同时变红。

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

🔴 **2026-10-02 追记（分支 `qwen38maxkhovvob4/dep-ratchet-deadcode`）：上面这段读数已过时两处，如实更正**

1. **基线值变了**：2026-10-01 由 6 上调到 7（G3 那条 `golang.org/x/crypto`，组长台账 `4496f6c5` 准），
   所以现在的对照是「直接依赖 8（基线 7）」，不是「8（基线 6）」。
2. **多出的两条已清掉一条**：`github.com/minio/minio-go/v7` 是**死代码带进来的**——
   `a1d3a70e`（2026-09-11 加 MinIO 适配）之后紧跟 `8eabfe07`（2026-09-16 改用本地文件存储），
   此后 `NewMinIOObjectStore` **全仓零调用方**（实测：整仓只剩它自己那一行定义），
   生产只走 `NewLocalObjectStore`（`cmd/file-service/main.go:50`、`cmd/file-service/verify_retention.go:31`），
   `deploy/H-FILE-SERVICE-DEPLOYMENT.md:10` 明写「MinIO is not deployed」，
   `deploy/test_file_service_deploy.py:98-100` 还断言部署面不得出现 `FS_MINIO_`／`minio.service`。
   ⇒ 删 `internal/fileservice/minio_store.go` 与 `minio_store_test.go`（后者只拿进程内 `fakeMinIO`
   测那具死代码；同一份契约由 `local_store_test.go` 的 10 个格覆盖：原子落盘、不可执行、拒绝既有目标与
   符号链接、失败不留半成品、删除规则、保留期），再 `go mod tidy`：minio-go 连同 6 条传递依赖
   （go-ini／goccy-go-json／klauspost compress＋cpuid／minio md5-simd／rs xid／x-net v0.30.0）一并移出
   go.mod／go.sum。实测 `go build ./...` rc=0、`go test ./internal/fileservice/ -count=1` rc=0、`go mod verify` 全过。
   ⚠️ tidy 还会往 go.mod 添一行 `github.com/stretchr/testify v1.9.0 // indirect`：那是 `pgx/v5/pgconn` 的**测试**依赖
   （`go mod why -m` 实证路径），原先由 minio-go 的剪枝图供给（minio-go 自己的 go.mod 里就写着
   `testify v1.9.0 // indirect`），minio-go 一走就得显式记账。**版本没变、go.sum 里它的两条哈希本来就有**，
   不是新引入模块，也不进本闸的计数（只数非 indirect 行）。

⇒ **现存只剩 `golang.org/x/sys` 一条**，而它不是新依赖，是**被误标的真直接依赖**：
   `internal/m5/blite_linux.go:53,61` 用 `unix.PidfdOpen` 与 `unix.Syscall(unix.SYS_PIDFD_SEND_SIGNAL, …)`，
   `internal/fileservice/local_store.go:142,227` 用 `unix.Renameat2(…, unix.RENAME_NOREPLACE)`。
   标准库换不了：go1.22.2 的 `go doc syscall` 实测只有 `Renameat`／`Flock`／`Fchmod`，
   **没有** `Renameat2`、`RENAME_NOREPLACE`、`PidfdOpen`、`SYS_PIDFD_SEND_SIGNAL`
   （少了 `RENAME_NOREPLACE` 就毁掉本地存储「不覆盖既有对象」那条判据，少了 pidfd 就有 PID 复用误杀风险）。
   登记它＝上调基线（评审面）——**组长裁定 `2026-10-02T12:44:05Z` 已准**（原文「x/sys 基线 7→8 准（标注修正）」），
   故本分支同批把 `specs/gates/dependency-baseline.txt` 的 `direct_count` 由 7 改到 8、`direct_sha256` 换成新集合
   （原 7 项 ＋ x/sys，已用闸自己那套 awk｜sort -u｜sha256sum 复算一致），三段论证按 x/crypto 先例
   （`ruling-2026-10-01T101757Z` 六-1）写在该文件注释里。
   🔴 **登记与删除不可分离合入**：只合基线 ⇒ main 上仍是 9（minio-go 还在），红；只合删除 ⇒ 8 vs 7，也红。

🔴 **顺带实证一道闸的强度**：把 x/sys 那行手工标成 `// indirect`，本闸立刻变绿（7 vs 7）；
   但紧接着 `go mod tidy` 会把标注改回来（改回后 go.mod 与 tidy 结果逐字相同）。
   ⇒ 这个「8」糊不过去，只剩两条路：删依赖，或按 §4.7.1 ⑤ 论证后上调基线。

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
6. **工具构建缓存的旧产物不自动清理**：内容哈希定名 ⇒ 工具源码每换一版就多一个约 8.8MB 的产物。
   故意不清：清理会与并发跑的其它工作树打架（它可能正在执行那个文件，撞 `ETXTBSY`）。
   要清就人工清整个目录 `~/.cache/dingwei-pgtest`。
7. **进程被硬杀（SIGKILL／整条进程组被杀）时清不干净**。优雅路径（关 stdin、SIGINT/SIGTERM）
   会停库、删运行目录与日志目录、收掉孤儿；但工具自己被硬杀就来不及收尾。
   本席实测过一次：后台跑 `-count=50` 的作业随 shell 退出被杀，留下
   `/tmp/dingwei-pgtest-4099956023`、`/tmp/dingwei-pgtest-log-3911362584` 与 1 个 postmaster 僵尸；
   两个目录已手工清掉，**僵尸清不掉**（只能由父进程收，而 PID 1 是 `sleep infinity`）。
   同一条边界也适用于「起库失败」：那时助手**故意不删**两个目录（PG 日志是失败原因的唯一现场），
   改为把路径打进报错文案，由人看过之后自己删。
8. **残留的独立核对按【增量】判**，看不见本次之前就存在的残留（理由见第二节末）。
   本席这台容器上目前挂着 27 个历史僵尸（修复前攒的）与 26 个历史 `embedded_postgres_log*`；
   在**别的**机器上复跑时请先记一次基线，别把它们算到本次头上。
9. **`PR_SET_CHILD_SUBREAPER` 只在 Linux 生效**（`reap_linux.go`）；非 Linux 为 no-op，
   僵尸那项核对也因 `/proc` 不存在而自动跳过——是**跳过**，不是「判过」。

## 八 · 交付面清单

| 文件 | 性质 | 说明 |
|---|---|---|
| `tools/pgtest/main.go` | 新增（独立 module） | 内嵌 PG 起停器；stdout 只走协议行，PG 日志全导 stderr；自己删运行目录**与日志目录**并自证（`CLEANED` 为两者的合成值） |
| `tools/pgtest/go.mod` | 新增 | 独立 module，头注写明为何不并进主 `go.mod` |
| `tools/pgtest/go.sum` | 新增 | 同上 |
| `tools/pgtest/reap_linux.go` | 新增 | `PR_SET_CHILD_SUBREAPER` ＋ `Wait4` 收尸（🔵④）；数值常量自带来源注明，不为此添 `x/sys` 依赖 |
| `tools/pgtest/reap_other.go` | 新增 | 非 Linux 的 no-op，保证 `go build` 在任何平台都过 |
| `internal/store/pgtest/pgtest.go` | 新增 | 主模块测试助手，**只用 stdlib＋`internal/repo`**，不给主 `go.mod` 添依赖；构建缓存按源内容哈希定名（🔴①）、Skip 放行开关（🟡②，默认已翻转为判 Fail，见五之三） |
| `internal/store/pgtest/residue.go` | 新增 | 「随起随清」的**独立核对**：临时目录日志文件与 postgres 僵尸的前后快照增量（🟡③／🔵④） |
| `internal/store/pgtest/pgtest_test.go` | 新增 | 10 条用例（其中 6 条是反向／正向对照），含 3 条端到端子进程对照 |
| `.gitignore` | 改 1 处 | 忽略本地构建产物 `/tools/pgtest/pgtest`，源码照常入库 |

未改：任何 schema、任何判据、`go.mod`／`go.sum`、`specs/gates/*` 基线、preprod。
