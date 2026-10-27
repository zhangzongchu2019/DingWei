# ④ Claude 显式启动命令闭合

后续watchdog修复及当前门结果见 [WATCHDOG-QUEUE-CLOSURE.md](WATCHDOG-QUEUE-CLOSURE.md)：当前严格Go门已过、完整helper仍有清理错误。本页保留59b5479历史失败证据，不作为最新上线结论。

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

取证截止 `2026-10-27T10:33:53Z`；受测代码 `59b5479f8602ffc73d1908e31b11483a22aab651`，tree `df8a044542dfb2cebdcbd4970baef1a7a5ea7ca7`。最终文档提交不改产品代码。

| 门 | 实际结果 |
| --- | --- |
| 命令回归 | rc0，8项 |
| 相关回归 | rc0，54项 |
| 去补齐负控 | rc1，a/c两格均红在指定partial flag文案 |
| 真实显式Claude B7 | rc0，33.226s，判据1–5通过 |
| helper 全套 | rc0，1027项，439.849s |
| go build ./... | rc0，Go1.22.2 |
| 构建入口合同 | rc0，14项 |
| run-build-tests | rc0，实际Python67组／shell2组 |
| 严格m8＋store全包 | **rc1**，顶层PASS1037／FAIL1／SKIP7；含子测试PASS1547／FAIL2／SKIP7 |

完整门的唯一顶层失败是 `TestDeliveryACKActualCLIQueuedAdmissionAndTerminal` 的 Codex 子格，详见下节。不能用Claude必需格通过替代完整Go门。最早未提交工作区的一次尝试被升级打包测试以 `source_not_clean` 拒绝，未计作产品行为结论；提交后的两次干净完整运行，升级打包门通过，但均在下述Codex格失败。

原 r40b 交付读数、既有 C2/G2 与基线同红和7项环境/既有 SKIP 见 [DELIVERY.md](DELIVERY.md)，本件不并入范围外修法。新增真实显式命令变体使 B7 由14格增至15格，仍为两种实际 driver。


## 新问题登记：Codex 第二回合接纳超时（未定因，不出修法）

- 干净候选59b5479的两次完整门及一次原样Codex单格复跑，均在释放A后等待B admission的判据3超过60秒；完整重跑rc1，单格复跑rc1（93.254s）。未继续反复试到绿色。
- 实际快照：A的原生terminal已到；事件drained包含A；Hub已收到A的processed；本地队列current仍为A、driver state为processing，B未产生模型请求。保留原ACK、原生观测、helper栈与debug-state文件；这里只描述观测，不认定根因。
- 一次性干净工作树独立取本次修复前锚7ab9460，原样同格对照，本轮rc0（33.901s）。不能据此将候选失败直接定性为既有问题，也不把锚点一次通过当作候选通过。
- 本次没有改Codex产品路径，但完整门仍未通过。按范围冻结，仅登记、报告组长和交叉席，不并入新修法。**当前r40b不具备上线结论；还须关闭该门并取得togpt60独立交叉PASS及业主授权。**

## 日志SHA-256

本机证据目录 `/data/ai-dev-data/r40b-implementation-20261027-evidence`，未上传源码或制品。

| 文件 | SHA-256 |
| --- | --- |
| `r40b-claude-launch-contract-final.log` | `dd620763a11e61eb1509945b3d2d01a82e7b15d9ebcdffe15734554e86b295aa` |
| `r40b-claude-launch-related.log` | `74b4941cf8aa2d997120a4606cd5bf4263419ad59807da3805e0bdd1ac59933d` |
| `r40b-claude-launch-mutation-final.log` | `63cce5322091cdb666e73f4d0b269e6ad2f1cd6c796ca43d9d82cb55f5f03645` |
| `r40b-claude-explicit-real-final.log` | `f3e3b0b9ceb5bde122655966b40a07778a6feba1fa650aa81b9a27a4df8c7569` |
| `r40b-claude-launch-helper-final.log` | `8bd247dbf6b27fc3f75c77210cd9b51dbc3e447435904dddcec12c4b0522864c` |
| `r40b-claude-launch-build-tests-accepted.log` | `f0e186ef22ab36abd14edb678c09cb58068fb1ba8f21804002b7b281afd96e3d` |
| `r40b-claude-launch-build-entrypoints.log` | `62e8da33f9a3f7521deb23bd86635a4fc727ec2cf94a42e903543d26cd2738a7` |
| `r40b-claude-launch-build.log` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `r40b-claude-launch-go-strict-clean.log` | `dd53f445d465bf5dda0d466be36a2f4ab89fee069ddb0f875d7d081acbb9fc5a` |
| `r40b-claude-launch-go-strict-repeat.log` | `78075edfab442fde67f39b88a51fee9b0c36de62e057f6eb80eb22cedf162417` |
| `r40b-claude-launch-codex-recheck.log` | `389b333d5d10bd34c9204a3ae59def947d88b5ed0a9ad4dff385282d63ee1999` |
| `r40b-codex-before-argv-anchor.log` | `ef29e76aba04820ae8e6b7fe38c935dec05274da42f5a13c7bd8cf9a06cb95d8` |
| `claude-launch-native/codex-002-debug-state.json` | `a026078d9f9119bfb21d5eaf336e2c26abc5ef7a4dbd7641df0825261f7053a2` |
| `claude-launch-native/codex-002-model-observations.json` | `985e6f65fa39288d14a8b1546b61bac53faee7ce8030fb3333ace0badd5c9468` |
| `claude-launch-native/codex-002-native-acks.json` | `3e098aeb74f0e463f90affa114b958b5e47b6b3749e59fbd9d8018a0eacab939` |
| `claude-launch-native/codex-002-native-confirms.json` | `3865333d702baabff281e7f00852be65aac532edbb3c435c8d88055f3b3f1c78` |
