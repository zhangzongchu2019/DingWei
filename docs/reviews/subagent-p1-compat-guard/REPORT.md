# S-SUBAGENT-P1-COMPAT-GUARD

基线：`opus50tc2k/r55-integration` `f2a5db5f`；先合入 `coder56solzzc/subagent-addressing-design` `d9a43e6a`，形成实现分支父提交 `d6657823`。本交付只做第一期兼容拒收，不提供子 Agent 创建、注册或投递能力，不触碰现网、预发或在线席位。

## 保护行为

- **AMR**：三段及以上 `@` 提及不生成投递候选；同一正文的合法根候选仍正常路由，发信方收到“当前不受支持，已跳过”的说明。派生信封正文仍从原信封原样复制。混合提及的实际路由测试逐字节比较正文并比较 SHA-256。
- **Hub**：第一期连接始终视为未协商。入站 envelope 的 `sub_path` 及设计定义的 `target_*`、`lane_generation`、`subtree_revision`、`from_*` 保留字段在 WS 入口及 `RouteEnvelope` 入口都触发 `sub_agent_unsupported`。Hub 不采信外部声称的路径/来源，也不把它降级到父会话。
- **send.py**：三段及以上目标在 `main()` 本地预检拒绝，提示“不受支持、未降级、未发送”；拒绝发生在 `ipc_send` 之前。两段根地址仍可发送。
- **AA-007**：规格与设计回写〔51〕-补10（rulings `89bda3bc`）批准值：完成即退开、空闲 1h、最长存活 2d、平台上限 5d 且按节点各自计、退出宽限 10min、父结束级联。墓碑保留仍是 PM-03。

## 混部行为

| 组合 | 拒收位置与用户可见行为 |
|---|---|
| 新 Hub + 旧 helper | 若旧 helper 仍发出三段 `to`，旧的完整三段目标解析会拒绝；若信封携带子路由保留字段，则新 Hub 回 `sub_agent_unsupported`，经现有投递失败通知告知不支持。两种路径都不转父。 |
| 旧 Hub + 新 send.py | 标准 `send.py` 调用在本地预检拒绝三段目标，提示“不受支持、未降级为父会话、未发送”，不会发 IPC。 |

旧 Hub 对“根地址 + 未知 `sub_path`”的已知误投边界仍存在；绕过新版 send.py、直接构造 IPC/信封不属于旧 Hub 的保护范围。因此本变更只为受新版入口约束的调用提供拒收，子 Agent 能力仍保持关闭。

## 验证

原设计 future 复现由两项目标红转为全绿：

- `AA005_CHILD_MENTION_NOT_SKIPPED_WITH_VALID_ROOT_RETAINED`：绿，保留合法根候选。
- `UNNEGOTIATED_SUBPATH_REACHED_PARENT`：绿，未知子路径被拒绝且父未收到。
- legacy 观察模式：旧的其余控制通过；以上两项新保护断言仅在 future 模式运行。

复现命令（各用独立临时输出目录）：

```sh
python3 tests/subagent-addressing-design/run_legacy_probe.py --future --output /tmp/subagent-future-output
python3 tests/subagent-addressing-design/run_legacy_probe.py --output /tmp/subagent-legacy-output
python3 tests/subagent-addressing-design/run_compat_mutations.py --output /tmp/subagent-mutation-output
```

变异逐项撤除护栏，预期红点及实得如下：

| 护栏 | 变异 | 预期红点 | 实得 |
|---|---|---|---|
| AMR | 让深层 token 进入候选并移除候选过滤 | `AA005_CHILD_MENTION_NOT_SKIPPED_WITH_VALID_ROOT_RETAINED` | rc 1，命中 |
| Hub | `rejectUnnegotiatedSubAgentEnvelope` 恒返回 nil | `UNNEGOTIATED_SUBPATH_REACHED_PARENT` | rc 1，命中 |
| send.py | 本地拒收条件失效 | `test_three_segment_target_rejected_before_ipc` | rc 1，命中 |

future / legacy `go_rc` 均为 0；future 两项目标断言全绿，legacy 两项新保护测试跳过、其余设计控制通过。变异汇总与三个原始日志在 `raw/mutations/`；future 与 legacy 原始 Go 输出及结果 JSON 分别在 `raw/future/`、`raw/legacy/`。

实现中 Hub 保留字段表曾包含通用 `generation` 等名称；复核全仓使用点后，在完整测试前将其收窄为设计 §3.2 明列的八个身份/路由字段，避免截获其他协议字段。另一次完整 m8 曾返回非零且终端输出截断、未留原始日志；随后可复现的一次完整运行在两条 AMR 日志卫生断言上失败（见 `raw/m8-final-failed.log`）：深层 URL/email 片段被预算过滤，遗漏原有非法 token 日志。现已改为仅过滤语法有效的深层提及，非法 token 仍走原日志路径；最终整包通过。

门禁读数与原始输出：

- m8 整包 `go test ./internal/m8 -count=1`：rc 0，225.703s；原文 `raw/m8-final.log`。
- `bash scripts/run-build-tests.sh`：用 `requirements-runtime.lock` 匹配的 Python 虚拟环境、剥离继承的 SH_/DINGWEI_/DW_ 环境变量、短 TMPDIR；两 profile 每个 1129/1129，27 Python、2 shell，rc 0；原文 `raw/run-build-tests-final.log`。
- OpenSpec 1.16.0 `validate --all --strict --no-interactive`：27/27，rc 0；原文 `raw/openspec-strict.log`。

早期使用系统 Python 的 `run-build-tests` 尝试在 helper 测例发现阶段 rc 1，原文保留在 `raw/run-build-tests-system-python.log`；随后使用与 `requirements-runtime.lock` 匹配的环境完整通过，以上结论只采用锁定环境结果。

## 范围外

保留字段清单按设计 §3.2 的信封路由/来源字段限定；Hub 不以通用 `generation`、`node_id` 等可能由其他协议使用的字段名扩大拒收面。没有开启任何子 Agent 能力，也没有改变普通根地址路由或现有提及测试判据。
