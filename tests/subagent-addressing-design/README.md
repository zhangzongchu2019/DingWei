# 第一期隔离复现

从仓根运行（普通用户，不运行在线实例）：

```sh
PATH=/tmp/coder-0232-locked-venv/bin:$PATH python3 -B tests/subagent-addressing-design/run_legacy_probe.py --output /tmp/unique-legacy-output
PATH=/tmp/coder-0232-locked-venv/bin:$PATH python3 -B tests/subagent-addressing-design/run_legacy_probe.py --future --output /tmp/unique-future-output
python3 -B tests/subagent-addressing-design/native_parallel.py --binary /path/to/pinned-0.159.2/codex --output /tmp/unique-native-output
```

每次用新的输出目录。兼容保护实现后，future 模式要求 AA005 与未协商 `sub_path` 两项断言都绿；legacy 模式跳过这两项新保护断言，其余历史控制仍须通过。Go通过overlay挂载`.go.txt`，仅此命令关闭vet以免vet打不开虚拟新增路径；不创建产品测试文件。`run_compat_mutations.py` 分别移除 AMR、Hub、send.py 护栏并要求对应断言变红。native仅loopback假SSE，不调用真实provider，二进制SHA硬校验，不读取在线席位secret/模型配置。二进制本身不随交件上传。

raw原文与边界见docs/reviews/subagent-addressing-design/REVIEW.md；设计见docs/design/SUBAGENT-ADDRESSING-PHASE1.md。schema中的实验dynamicTools不代表已经实现可信发信；需后续原生工具调用负测。
