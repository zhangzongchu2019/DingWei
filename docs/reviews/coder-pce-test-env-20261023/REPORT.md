# PCE A3：无 Python pidfd API 的测试夹具修订

执行源 `6139e4bca7650697682d89c85fa217c0459ca890`，tree `bc16bc5d46d3a665d70dd26227aed5f7b334e025`，唯一父提交为指定基线 `integration/spec5ff-r38-1@f1aabc6cbe3edbe887760f65e53a76a7b4062252`（tree `e225acf9ba4856ed0e62e4ef93479f011dd3468e`）。仓库 `git.code.tencent.com:wego-ai/dingwei`，分支 `review/coder/pce-test-env`。后续证据提交仅增加本目录；以交件消息中的最终 tip 为准。

## 定因与修改

按组长后续裁定，本件只处理 A3：`pce_retention_crash_fixture.py` 假定解释器提供 `os.pidfd_open`。prodops 的私有 CPython 3.12.3 没有该可选 API，导致真实 owner 崩溃用例在建立 pidfd 时失败。对应外部取证为 `review/prodops/r38-rollout-plan-20261023@7004297d793c55a7dac452bea0550e270b1f1565`；余项扫描为同分支 `a4eec63834ab6b1f3c6aed1d5124a81b0c28bfe4`。A2 可执行文件属主前置条件已另行闭合，不在本件修订，产品 fail-closed 检查保持原样。

仅改两个测试文件：

- 夹具有 API 时继续使用原来的 pidfd 打开、信号及回收路径；API 存在但调用报错仍报错，不把任意失败降级为成功。缺失 API 时，先以认证绑定中的 child starttime 比对 `/proc` 身份；owner 被 kill 并 wait 后，原有 `native_call` 仍校验该子进程身份并确认协议存活，再执行原有 reclaim 拒绝断言。
- 回退清理限定于这个独立夹具进程。它本来就是 subreaper；owner 退出后先 `waitpid(WNOHANG)` 证明目标已为本进程的子进程。已回收则结束；未退出则再比对 starttime，然后发 SIGKILL 并最多轮询 5 秒回收。未被其他线程回收的自有子进程即使恰好退出，也会保留 PID，避免核验到信号之间的 PID 重用。非子进程直接抛错，身份变化直接拒绝，均不发信号。没有全机扫描或根据 PID 文件终止进程。
- 新增无 API 的真实 owner 崩溃用例，以及拒绝非子进程／starttime 变化的回归格。原有 `native_survived_owner`、`pin_retained`、`child_reaped` 三项断言完全保留；没有新 SKIP。subreaper 标志只在独立夹具的 `run()` 内设置，不在 unittest 主进程中设置；单元格 `runpy` 导入夹具也不会运行它。

## 原故障复现与门测

无 API 仿造只在夹具子进程删除 `os.pidfd_open`，不修改系统解释器、锁定 venv 或产品。原版在同一行报 `AttributeError`、rc1；修后 rc0，三项结果均 true。有 API 的本机系统 CPython 3.12.3 路径由原用例和 canonical 验证。实际 dw123 的私有解释器复跑由 prodops 独立交叉，本地仿造不冒称目标机执行。

原版在建立 pidfd 前失败，原清理代码因此遗漏其 native 子进程。复现由独立的外层 subreaper 托底，只对自己收养且 `waitpid` 可证明归属的测试子进程回收；原版回收 1 个，修后为 0。该标志不作用于测试主进程或在线 helper。[独立基线与修后 worktree 的前后源哈希及结果](raw/COMPARISON.json)、[原始异常](raw/comparison-before.stderr)、[修后结果](raw/comparison-after.stdout)、[托底脚本](raw/missing_api_probe.py)。

正式门均绑定冻结执行源及测试／产品文件 SHA，使用清空继承环境后的 allowlist、隔离 HOME、短 ext4 TMPDIR。锁定 venv `/tmp/coder-0232-locked-venv`，runtime lock SHA `5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0`；包版本与锁一致。[环境 provenance](raw/PROVENANCE.json)、[执行器](raw/run_gate.py)。

| 门 | 结果 | 原始证据 |
| --- | --- | --- |
| retention，有／无 API 与拒绝非自有 PID | 29 项 rc0 | [retention](raw/retention.json) |
| canonical | Python 63、shell 2，rc0；清单未改 | [canonical](raw/canonical.json) |
| helper 全套 | tests discovery 963 项 rc0；根目录 discovery 另 9 项 rc0 | [963 项](raw/helper-tests.json)、[根目录 9 项](raw/helper-all.json) |
| 全仓 Go（严格嵌入式 PG，包串行） | 53 个测试包、13 个无测试包，rc0 | [Go](raw/go-all-valid.json) |
| vet | rc0 | [vet](raw/vet.json) |

## 撤修绑定

五刀在独立 worktree 执行，门测 worktree 未变动。每刀逐项保存原／变异／恢复 SHA、命令、隔离环境、UTC、原始 stdout/stderr；均 `0→1→0`，原 SHA＝恢复 SHA＝正式执行源当前 SHA。[完整记录](raw/mutations/SUMMARY.json)、[执行器](raw/run_mutations.py)。

| 刀 | 撤掉的检查／行为 | 红格 |
| --- | --- | --- |
| M1 | 可选 API 探测改回直接访问 | 无 API 同行 AttributeError |
| M2 | 回退分支的回收 | 原有 `OWNER_CRASH_FIXTURE_LEAKED` 断言；外层只回收遗留的 1 个自有测试子进程 |
| M3 | 忽略 `waitpid` 非子进程错误 | 非子进程必须拒绝的断言 |
| M4 | 回收前 starttime 比对 | 必须报 `OWNER_CRASH_CHILD_CHANGED` 的断言 |
| M5 | 原产品 reclaim 的 stopped／stop-proof 两道拒绝 | 原真实 owner 崩溃用例、有／无 API 两路均红；不再返回要求的 `retain_stop` |

M5 只在独立变异 worktree 撤产品门后恢复；交付产品树不变。最终六门、对照复现及五刀绑定均由 [核验脚本](raw/verify_evidence.py) 检查，[核验结果](VERIFY.json) 为 PASS。[源码与恢复核验](raw/SOURCE-VERIFY.json)。脚本准备阶段的 Go 路径误写及 M3 选择器不唯一均如实登记在 [HARNESS-NOTES](raw/HARNESS-NOTES.json)，不计为产品测试失败或有效撤修红。

## 边界

没有产品修改、Hub 迁移、版本位、依赖锁或 canonical 文件计数变化；不放宽 PCE 可执行文件或身份门。此候选只解决解释器缺少可选 API 的夹具假设；仍要求 Linux `/proc`、subreaper 及原有身份验证能力，缺失时继续失败。

没有运行 `build.sh`、没有发布二进制、没有部署、没有修改生产环境或重启。A2／A3 旧 GO 均保持作废；候选仍需组长审、prodops 交叉，合 r38.2 后另行新 GO。原备份未触碰。
