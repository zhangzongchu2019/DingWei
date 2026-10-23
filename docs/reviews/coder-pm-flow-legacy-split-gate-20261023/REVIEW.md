# S-PM-FLOW-LEGACY-SPLIT-GATE 交审

基线：review/togpt60lunazzc/pm-spec-flow-impl 的精确 3ffc121d20de8a2f3ecc39042fec5075dc3fe891；不跟随远端后续 tip。设计依据 2a6d3260b3a877b6cb0b25709741ea6edef61d2a 的 docs/designs/S-PM-SPEC-FLOW.md 第18行。执行源码冻结于 1d7b06fec69e9d7aedbb7939eba81b05df35a0bb，tree 073d83ca325969251aa93d3dc536526f6f5b2bfa；本交审证据提交只追加文档，两个执行文件 SHA256／blob 见 VERIFY.json 与 raw/SOURCE-BINDING.json。

## 改动与兼容边界

internal/store/console_spec_pm_flow.go 的 requireFinalizedPMSpecForSplit：enabled=true、flow表存在但 spec 无行时，返回 ErrPMSpecFlowDenied。旧 final spec 不补写流程历史，须派生新修订后走已审 PM 流程。enabled=false 的早返回、迁移表缺失时返回 nil、其他错误和已登记 final 校验均未改；没有扩展权限、改迁移、改页面、改自动投递或发版。

internal/store/console_spec_pm_flow_legacy_gate_test.go 新增两格：

| 格 | enabled=true | 同一 spec / candidate enabled=false |
|---|---|---|
| TestPMSpecPresplitRejectsUnregisteredLegacyFinalSpecOnlyWhenEnabled | 精确 ErrPMSpecFlowDenied，数据库无 presplit 请求 | 原请求ID可创建，返回指定 spec / agent，恰一请求 |
| TestPMSpecCandidateConfirmRejectsUnregisteredLegacyFinalSpecOnlyWhenEnabled | 精确 ErrPMSpecFlowDenied，零任务、candidate 未确认、recheck 未触发 | 原 candidate 可确认，任务数匹配、confirmed有效、recheck恰一次 |

另增缺迁移表的兼容性格，enabled两值均返回 nil。既有格断言未调整。测试 fixture 仅新增 flowState 为空时不登记 flow 的分支以构造真实 legacy 情形；原非空调用仍按原样插入，没有修改原格期望。

## 撤修与门禁

新测试加到原产品实现时，两格均在“错误返回 nil”断言红（raw/tests-before-fix.log）。候选先定向绿，再以 Go overlay 只撤掉 ErrNoRows 分支的新 denial（返回 nil，保留候选注释与所有测试），分别单独跑两格，二者各自红，恢复候选后 store/m8 定向全绿。overlay 源文件只留本机，未放文件服务；变异 SHA 与实际命令见 raw/withdrawal-binding.json、withdraw-presplit.json、withdraw-candidate.json。

| 门 | 命令／结果 | 原始记录 |
|---|---|---|
| store/m8 PMSpec / LegacyR8 | go test ./internal/store ./internal/m8 -run 'PMSpec\|LegacyR8' -count=1 -v；21格 PASS | raw/restored-targeted.*；raw/targeted.log 保留首次绿 |
| presplit 一刀撤修 | 单独目标格，rc1，命中 enabled 无行却 nil | raw/withdraw-presplit.* |
| candidate 一刀撤修 | 单独目标格，rc1，命中 enabled 无行却 nil | raw/withdraw-candidate.* |
| 全仓 Go | go test -p 1 ./... -count=1 -timeout=30m；rc0，53包有测试PASS／13包无测试，621.931秒 | raw/go-all-short-serial.* |
| vet | go vet ./...；rc0 | raw/vet-all.* |
| canonical | 原封不改 sh scripts/run-build-tests.sh；rc0，BUILD_TESTS_OK python=59 shell=2，125.785秒 | raw/canonical-short.* |

门禁使用受控环境，Go 1.22.2、Linux，PG STRICT=1／SKIP_OK=0／EMBEDDED=1；canonical HOME为本轮私有隔离目录，Python来自既有 /tmp/coder-0232-locked-venv，不接本席 helper 或生产服务。每个门的 UTC 起止、命令、白名单环境、rc、执行源 SHA 均在相应 JSON；不声明所有可选平台测试均未 skip，只报告实际命令结果。

## 首轮失败与复跑（不覆盖原件）

首轮全仓 go test ./... 在较长 TMPDIR=/data/ai-dev-data/tmp-coder-tb 下失败：cmd/workpulse 两格与 mcsd 一格 Unix socket 超长，另有 consolemux active SQL残留及 mcsd SQLite busy。raw/go-all.* 为原始 FAIL。未修改这些范围外文件；consolemux 单包复核 rc0，随后全仓采用短 ext4 TMPDIR=/data/ai-dev-data/t 串行 -p 1 完整复跑 rc0。后者是本件全仓通过的实际条件，不能将首轮说成通过，也不将串行复跑当作并发稳定性证明。

首轮 canonical 的桌面/claim子进程格失败（raw/canonical.*），子进程异常按既有策略仅输出脱敏失败，没有绕过该保护获取私密引导数据。长 TMPDIR 会使这几格 mcp.sock 示例路径长达111/113字节，超过Unix socket限制；属于路径相关推断，未声称取得子进程详细根因。仅换短 ext4 TMPDIR=/data/ai-dev-data/c 后，保持源码、59+2清单、隔离HOME及Python依赖不变，全清单重新运行并通过。没有改期望、提超时、筛掉失败格或把失败输出改成成功。

## 交付边界

仅两份执行源文件及本目录证据；无部署、集成、版本号、配置、迁移或生产数据变更。等组长审与prodops交叉。INBOX-LAG A 仍等r38推出和另令，不随此件开写。
