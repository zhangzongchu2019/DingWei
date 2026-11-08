# S-GATES-GREEN 自测证据

状态：本席自测证据已齐；指定门实际rc0，交独立复核，不代替验收。原始未完成整包race的主动中断单独登记，不计作通过。

基线：0fe09b9e（现网r53锚，完整值见RESULTS.json）。任务：组长2026-11-08T06:34:51Z，rulings 2275435d，㊾-补7更正锚c71bc54ec4a82f3aa8a73dfa7b26a8fbe3f76d5f。复验依据28611f8b的review/togpt60lunazzc/sqlite-to-pg-survey-verify-20261108/raw/gate-attribution.txt。归r54前置门，不是多活实施或部署授权。

唯一写者：01a1b57a-e610-71a2-ba84-6007a77776f4，原子锁/data/ai-dev-data/s-gates-green.writer.lock，取得于2026-11-08T06:35:02Z。分支coder56solzzc/gates-green；私有仓git@git.code.tencent.com:wego-ai/dingwei.git。后续推送、远端核对、交件回执完成前不释放锁。

## 提交对应项

| 提交 | 对应项 |
| --- | --- |
| 748b02226 | 五处建库增量逐项处置；内存schema校验改repo，四处精确文件例外；open基线5保持，显式schema SystemCtx16→17附原因 |
| d118e32d2 | go-pinyin v0.21.0突破三栏、MIT/零传递依赖、直接依赖8→9及集合hash；不改算法/go.mod |
| 1909a8f46 | schema触发口径独立提交；Go AST字面串+SQL DDL识别，JSON/查询引用不冒充列；真实7列保留store/extseat要求 |
| 8b915fe52 | extseat.IssueToken跨tenant拒绝的最小审计及四项负测；真实PG持久化/存在性夹具 |
| f3d0a4998 | store.StageClarificationInbound元数据前置拒绝最小审计及四项负测；真实SQLite连接器spy |
| 本交件最后提交 | 三闸接入run-build-tests.sh，失败先于helper且保留真实非零码；新增入口行为契约测试、完整原始证据及规格进度 |

五处原增量：兼容探针main.go两处（必须种旧final状态/检查ledger）、启动配置测试一处（独立损坏history夹具）、history仓储一处（原始显式事务/写队列生命周期无法由现有回调Pool无损替代，本单不重写）、迁移校验一处改repo。另有**新拒绝负测**的连接器一处OpenDB，单列精确测试文件例外用于真实SQL spy；不是隐藏第六处产品建库。允许清单变更属评审面，机器不能自行识别审批人。

## 闸读数与作用域

- repo-boundary：open=5（基线5）；escape=1；pending=90；SystemCtx=17。本次转绿指**不新增未登记债**，不等于既有债归零。
- dependency：直接依赖9，集合sha256=e2018c8362ef4435b4f9d74277ef9642e311d034702db0d4e313b94dd6b58ef0。MIT原文与上游go.mod见dependencies/；pinyin-deps.log只有该模块本身一个非标准库包，传递模块0。
- tenant：DDL真命中7列：0062迁移4列、0074迁移2列归store，0105中np_external_principal.tenant_key归extseat。feishu/webhook.go、feishulogin/types.go仅JSON字段，不再误触发。表归属清单不是豁免。两域标记绑定真实测试，不把“字符串存在”当成隔离语义已全验。
- schema检测范围是字面CREATE TABLE/ALTER TABLE ADD列声明，支持普通引号、多行、注释剥离及字面拼接；不是完整SQL方言解释器，不执行运行期DDL拼装。原始命中与自测见raw/schema-hits.log、tenant-selftest.log、core-count3.log。

## 拒绝审计与负测

两处只增拒绝路径写入，复用原audit_log，不加表/迁移。extseat派生tenant取服务器配置，源principal取provider/issuer/subject摘要；store未通过元数据校验时无可信绑定，派生tenant明确记unresolved，源principal取issuer/subject摘要。固定op、deny、服务时刻；target为空，不回显调用方tenant、目标、正文、令牌。审计写失败/设施未配置仍保留原拒绝，只写固定错误日志。

四项分开测试：资源解析/非审计SQL spy=0；原错误对象及零返回（不成功空集）；真实存在/不存在夹具对比返回及五个审计字段同形；最小字段齐备及敏感输入不出现。两接口均无HTTP响应写口，本次改动不触碰状态码/正文/头的映射；方法级用例钉住原error对象/文本及返回值，m8整包与统一入口跨运行时用例验证上层既有响应。**没有拿方法级断言冒充一次新的HTTP端到端覆盖**。

上述四项针对两处明确授权的保护路径；不宣称所有tenant表、全部读写路径或SP7b隔离已完成。机器证明不做目标解析，不宣称网络时延统计上完全不可区分；时延仍归独立评审。

## 运行环境与源绑定

普通用户dev，Go1.22.2；子进程环境从最小字典构造，未继承SH_/DW_/DINGWEI_/DATAD_、代理或真实席位配置。PATH首位/tmp/coder-0232-locked-venv/bin，运行时websockets15.0.1；helper框架另设临时HOME及两种driver。没有生产、真实Agent、部署、重启或文件服务上传操作。

正式长门在f3d0a4998父锚上加两份已冻结的入口文件（scripts/run-build-tests.sh与deploy/test_build_entrypoints.py）运行；原始JSON如实记working_status与diff hash，**不声称父锚本身含入口改动**。1947份代码/测试/脚本/闸配置的逐文件SHA256记录于raw/FROZEN-SOURCES.json。最终收集器逐项核字节一致；最后提交只加入这些原样入口文件、文档和证据。后续文档调整不改变已测代码。复核可在最终提交直接逐项重算manifest。

## 门结果

实际门结果（原始命令、起止UTC、最小环境、真实退出码见RESULTS.json与raw同名JSON）：

| 门 | 结果 |
| --- | --- |
| 三个独立闸 | 各rc0 |
| go build ./... | rc0 |
| m8整包-count1 | rc0 |
| store整包-count1 | rc0 |
| store/extseat/schema工具/feishulogin四包各-count3 | 全部rc0 |
| store完整清单race3分8片 | 8片各rc0；379项×3=1137轮，1122 PASS/15 SKIP/0 FAIL |
| extseat整包race3 | rc0；14项×3=42 PASS/0 SKIP/0 FAIL |
| run-build-tests完整入口 | rc0 |
| 一次性树撤销变异后完整入口 | rc0 |
| 仓储/依赖/schema闸自测及OpenSpec strict | 全部rc0 |

普通构建与race构建的379项清单相同；8片实际运行集合并集与清单逐行相同，diff -u raw/race-list-all.txt raw/race-list-union.txt实际rc0，raw/race-inventory.diff为0字节；无重复分配。每例run=3，各片PASS/FAIL/SKIP总计见下表，跳过名与条件见raw/RACE-SKIPS.md。隔离依据见RACE-ISOLATION.md。


统一入口两轮均已有实际rc0：helper两profile各90文件、1097实际/1097预期（1088+9），Python27文件、shell2；本地真实Hub/客户端六项由shell单独设置隔离Python运行（含真实临时PostgreSQL），不使用真实飞书。普通整包的既有跳过条件/子用例逐项列在SKIPS.md；新拒绝测试没有Skip，未跑的专项不包装为PASS。

## 变异

| 变异 | 对应测试/闸 | 真退出码 |
| --- | --- | --- |
| extseat删审计写 | TestTenantDenyMinimalAudit | 1，minimal deny audit missing |
| store删审计写 | TestStageDenyMinimalAudit | 1，deny audit missing |
| 非例外路径新增建库调用 | run-build-tests入口 | 1，open5→6，helper尚未启动 |
| 撤销建库变异 | 同一一次性树run-build-tests | 0，完整helper/Python/shell均完成 |
| 两域各自提前读资源 | 各自PEP spy测试 | 各1，查询spy非零 |
| 两域各自把deny改成成功 | 各自deny语义测试 | 各1，原拒绝响应变化 |
| 两域各自把目标写入源principal以区分存在性 | 各自存在/不存在同形测试 | 各1，同形断言失败 |

共9个有归属的红变异，恢复后全入口绿；raw/MUTATIONS.json与CRITERION-MUTATIONS.json逐项附实际命令、sha和归属失败原因。所有变异仅在一次性工作树，恢复后核字节一致；无原树写入。raw/actual-*.py保留本次实际执行脚本（本机绝对路径按复核环境调整）。

## 原始日志

完整原始本地目录/tmp/s-gates-green-20261108；仓内副本raw/。RAW-MANIFEST.json记录每份原文sha256/字节数；超过500000字节的文本分片原样保存，按parts顺序拼接后核sha，不删段、不以摘要替代raw。最终门日志分别为build-final、boundary-final、dependency-final、tenant-final、store-final-count1、m8-final-count1、core-count3、race-count3、canonical、mutation-reverted-canonical。早期聚焦/初轮日志保留但不冒充冻结源正式门。

自测证据交独立复核，不代替组长或门方验收。

## store race逐片计数

| 片 | 顶层用例数 | PASS | FAIL | SKIP | 总轮次 | rc |
| --- | --- | --- | --- | --- | --- | --- |
| 0 | 48 | 144 | 0 | 0 | 144 | 0 |
| 1 | 48 | 144 | 0 | 0 | 144 | 0 |
| 2 | 48 | 141 | 0 | 3 | 144 | 0 |
| 3 | 47 | 141 | 0 | 0 | 141 | 0 |
| 4 | 47 | 141 | 0 | 0 | 141 | 0 |
| 5 | 47 | 138 | 0 | 3 | 141 | 0 |
| 6 | 47 | 135 | 0 | 6 | 141 | 0 |
| 7 | 47 | 138 | 0 | 3 | 141 | 0 |

合计379项，PASS1122/FAIL0/SKIP15=1137，全部3轮；5个既有顶层Skip各3轮，未新增跳过项。extseat独立全包42轮PASS。

## 原始整包race处置

原命令go test -race ./internal/store ./internal/extseat -count=3 -timeout=120m未跑完。所有被允许分片通过且矩阵核齐后，在2026-11-08T07:21:27.842837+00:00核对UID、父子关系、cmd与进程start ticks，仅对本次store.test PID 2282349发SIGINT；Go工具/runner随后结束，真实rc=1。该原始进程**没有计作rc0**。完整日志保留在raw/race-count3.log，处置与核验见raw/ORIGINAL-RACE-PIDS.json和ORIGINAL-RACE-DISPOSITION.json；未发现断言失败或DATA RACE签名，清理仅限该PID打开的本次/tmp/Test*夹具目录。此处不是用分片掩盖已发生的测试失败。

证据固定UTC：2026-11-08T07:22:13.280445+00:00。产品源1947份文件SHA仍全同；推送及交件的实际传输回执另记本席恢复件。
