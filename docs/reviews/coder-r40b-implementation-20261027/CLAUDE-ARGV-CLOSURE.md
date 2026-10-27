# ④ Claude 显式启动命令闭合

本件基于 `7ab946030395101d67423d94705b3615e8b2443a`，按组长追加要求处理④合同，提交只追加。无部署、helper 重启或席位 env 修改；togpt60 独立交叉仍为上线前必需门，本席自测不替代该门。

## 启动行为

在 Claude pipe driver 的实际启动 argv 构造处统一处理；调用方参数列表与环境变量原文均不改。

- 默认命令、显式 `cmd`、`SH_CLI_CMD`、配置 `cli_cmd`、`SH_CLI_BIN` 与 `SH_CLAUDE_BIN` 都进入相同规则；支持生产同形 `env IS_SANDBOX=1 …/claude.exe --model opus --dangerously-skip-permissions`。DriverAdapter 不再忽略配置的显式命令；显式已有 model 时保留。
- Claude pipe driver 需要双向 stream-json。缺少格式时补 `--input-format stream-json`、`--output-format stream-json`，以及 `-p`、`--verbose`、`--replay-user-messages`、`--include-partial-messages`；两项格式与 verbose/replay/partial 三个协议布尔 flag 归一，已含不重复，重复也折叠。已有 `-p` 或 `--print` 时不再添加 `-p`。
- 显式声明 text/json 等非流式格式，或为协议布尔选项提供冲突值时，选择**可读错误、Popen 前拒启动**，不把非流式参数静默改成流式。其它非流式适配器未改；非 Claude 可执行命令保持原样。
- 参数值、`env` 前缀、model、权限选项和 `--` 后的位置数据保留，避免把提示词中的字面 flag 当作启动选项。

**结论：r40b 上线无需改任何席位 env。** 修复由 helper 构造参数完成，不要求席位手填新增 flag。

## 判据与实跑

`test_claude_launch_contract.py` 检查实际传给 Popen 的 argv；已加入 `run-build-tests.sh` 标准清单。

| 判据 | 证据 |
| --- | --- |
| a 显式命令缺 partial flag | Popen argv 含且仅一次，并补齐其余协议参数；原命令列表不变 |
| b 已有 partial flag | 已有一次不重复，重复多次也折叠；重复归一幂等 |
| c 生产同形 env／claude.exe／opus 命令 | 保留原前缀和权限/model，补齐流式协议参数 |
| 各 env 来源／DriverAdapter 配置 | SH_CLI_CMD、SH_CLI_BIN、SH_CLAUDE_BIN 与 cfg.cli_cmd 均覆盖；非 Claude 原样、非流式拒启动 |
| 去补齐负控 | 一次性本地源码副本仅将归一返回值改为原 argv；a/c 两格均 rc1，红在“Claude实际argv必须补齐且仅一次 --include-partial-messages” |
| 真实 CLI | `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude_explicit` 使用显式 env 命令，不从 DEFAULT_CMD 取得协议 flags；真实 Claude、Hub、helper、View 判据1–5 rc0，33.226s，包含 A 长回合中 B 排队超过31秒 |

8 项命令回归通过；54 项相关回归通过。最终全套读数与散列在结束后收齐。

原 r40b 交付读数、既有 C2/G2 与基线同红和7项环境/既有 SKIP 见 [DELIVERY.md](DELIVERY.md)，本件不并入范围外修法。新增真实显式命令变体使 B7 由14格增至15格，仍为两种实际 driver。
