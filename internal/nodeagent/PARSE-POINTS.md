# nodeagent 外部输入解析点审计清单（r7 交付物）

- 承办：kimi30kqtuxerh（G4）· 2026-10-01 · 随分支 kimi30kqtuxerh/g4-m0-m1 提交，供 prodops 对 tip 核验
- 覆盖方法：按 `exec.Command` / `os.ReadFile` / `json.Unmarshal|Decode` / `Sscanf` / `Statfs` / `os.Getenv` 全量扫 internal/nodeagent 与 cmd/nodeagent，无遗漏
- 判读口径（组长 r7 令）：不是判"会不会 panic"，是判"**出错时是否静默放行**"。每处标【上抛/拒/跳过/放行】；凡跳过必附安全理由
- 结论：全包无一处"出错静默放行"

## 一 · xfs 命令输出（quota.go）

| # | 位置 | 失败时行为 | 跳过安全理由 | 用例 |
|---|---|---|---|---|
| 1 | Setup 的 lsattr 断言（lsattrOut） | 【拒＋回滚】err/空/畸形/号不符一律进同一 rollback（摘映射 project -C -p 全参数＋删目录），无 panic | — | TestSetupMalformedLsattrNoPanicFullRollback（空＋畸形）、TestSetupRollbackClearsProjectMapping |
| 2 | Clear 的 lsattr 读取 | 【上抛】err＋目录在/空输出/Atoi 失败一律报错、目录保留、零 xfs 命令；【跳过】目录已不在 ⇒ 幂等绿 | 目录不在则该项目无数据可清；项目号纵残留也仍在 report，分配器读 report 不会重分（保守方向，不漏双分） | TestClearMalformedLsattrFailClosed（五子例） |
| 3 | usedProjectIDs（report -p -n，分配依据） | 【上抛】井号前缀行 ID 读不出 ⇒ 拒分配（无法判定占用，防同号双分）；命令失败上抛 | 【跳过】仅无前缀行（表头/空行）：-n 模式项目行恒以井号＋数字 ID 开头，无前缀行不可能携带项目号。截断行（井号 ID 无用量列）ID 可读 ⇒ 照记占用，保守方向 | TestAllocProjectIDParsesReportAndRejectsCollisions（含 ID 解析失败拒分配、截断行照记两删闸子例） |
| 4 | projectUsage（D5 清后复查） | 【上抛】先判 ID 再判长度：目标行字段不足（截断）/用量非整数/命令失败一律解析错误 | 【跳过】不命中目标 ID 的行：复查只判目标 pid，其它行与判定无关 | TestProjectUsageReportParsing（含截断/非整数/不误伤三子例）、TestClearRedWhenProjectStillPresent（含截断行进复查 Clear 必红） |
| 5 | findmntTarget（findmnt --target） | 【上抛】出错带输出上抛、空输出显式报错；只 TrimSpace，无下标解析 | — | TestResolveMountPointNode68 |

## 二 · docker Engine 应答（docker.go）

| # | 位置 | 失败时行为 | 用例 |
|---|---|---|---|
| 6 | inspect 返回体 | 【上抛】畸形 JSON 上抛；200 但缺 Id 报错（不拿零值续跑）；≥300 出 engineError 带状态码 | TestEngineMalformedResponseFailsClosed、TestEngineMissingIDFailsClosed |
| 7 | create 返回体 | 【上抛】畸形上抛；缺 Id 报错（不拿空 ID 去 start/inspect） | TestEngineMissingIDFailsClosed |
| 8 | pull 返回体（images/create） | 【上抛】拉取失败以 NDJSON 流内 error/errorDetail 回报（HTTP 仍 200）——逐行扫流，非空即拒、畸形行即拒；【放行】非 error 的进度行（成功路径的正常输出） | TestImagePullStreamErrorSurfaced（流内 error／畸形行／正常流三形状） |
| 9 | 容器清单/镜像清单（capacity 用） | 【上抛】同 call 解码失败上抛 | TestCollectCapacitySeatsAndImages（正向）、TestEngineMalformedResponseFailsClosed（反向） |

## 三 · 本地文件

| # | 位置 | 失败时行为 | 跳过安全理由 | 用例 |
|---|---|---|---|---|
| 10 | 席位令牌文件（writeTokenFile） | 【上抛】写/chmod/chown 失败上抛；节点代理只写不读（读方是容器内客户端） | — | TestCreateSeatSecuritySpecAndTokenFile |
| 11 | 节点凭据文件（Config.secret） | 【上抛】缺失/权限宽于 600/空内容三拒 | — | TestSecretFileFailsClosed |
| 12 | 台账文件（loadLedger） | 【上抛】JSON 损坏报 errLedgerCorrupt（不静默清空重跑指令）；【跳过】文件不存在/为空 ⇒ 新台账 | 首次启动本无已执行指令，幂等台账只需兜住已执行的 | TestLedgerCorruptFailsClosed、TestLedgerIdempotentAcrossRestart |

## 四 · capacity 采集（capacity.go）

| # | 位置 | 失败时行为 | 跳过安全理由 | 用例 |
|---|---|---|---|---|
| 13 | meminfo（/proc/meminfo） | 【上抛】读不出 MemTotal 即报错（不拿 0 上报） | 【跳过】非 MemTotal/MemAvailable 行：只取这两键 | TestMemInfoMB（含畸形子例） |
| 14 | statfs | 【上抛】系统调用错误上抛 | — | TestDiskMB |

## 五 · 指令面（seat.go / run.go / agent.go）

| # | 位置 | 失败时行为 | 用例 |
|---|---|---|---|
| 15 | 指令参数 strictDecode | 【拒】未知字段一律拒（DisallowUnknownFields） | TestStrictDecodeRejectsArbitraryFields |
| 16 | 镜像引用 validateImageRef | 【拒】非固定仓库＋sha256 digest | TestValidateImageRef |
| 17 | 指令帧 JSON | 【拒该帧】畸形回 bad_command_frame、连接不断、后续指令照常 | TestBadCommandFrameReplied |
| 18 | 指令类型白名单 | 【拒】非白名单 unknown_command_type，不落台账 | TestAgentIdempotentAndReplay |
| 19 | Hub 证书钉证 TLSPinnedTransport | 【拒】指纹格式错/对端不出证/指纹不符三拒；相符放行 | TestTLSPinnedTransportRejectsMismatch；正确指纹全链 TestAgentIdempotentAndReplay、TestAgentAuthBindsIdentity |

## 六 · 环境变量（cmd/nodeagent/main.go）

| # | 位置 | 失败时行为 | 跳过安全理由 | 用例 |
|---|---|---|---|---|
| 20 | 必填 env（socket/cgroup 父切片/席位代理等） | 【拒起】缺省 log.Fatal；【跳过】NODEAGENT_LABELS 畸形对 | 坏对不含合法键值；标签仅作池调度分组，缺标签＝不入对应池（保守方向） | main 直读 env 无单测，行为即拒起 |

## 修复沿革（复验对照）

| 轮 | 提交 | 内容 |
|---|---|---|
| r4 | 87958eaf | Setup lsattr err=nil 空/畸形下标 panic → 进同一 rollback |
| r5 | 5d442248 | Clear lsattr fail-closed（err/空/畸形上抛）；纠偏 limit 注入不命中并补 limit 归零断言 |
| r5 补 | fbd076c0 | Clear 正向删闸：项目号 0（可验证无配额）才删目录、零 xfs |
| r6 | 594f220f | 五处补漏用例（docker 应答/钉证/台账/坏帧/meminfo） |
| r6 续 | 68fac53e | 真 fail-open 修复：imagePull 丢弃返回体 → callRaw＋逐行扫流；create/inspect 缺 Id 拒；凭据文件三拒 |
| r7① | 63f36b0d | projectUsage 先判 ID 再判长度：目标行截断/非整数上抛＋删闸 |
| r7② | 6f489420 | usedProjectIDs 井号行 ID 不可读上抛拒分配（防同号双分）＋删闸 |
