# FEISHU-COPY 交付结果

## 实现

- 基线：r56.4 集成 `af752f641`；迁移依据 rulings ㊿-补65，使用 PG `0111_extseat_feishu_copy.sql`。
- 外部 MCP→内部目标及内部目标→外部 MCP 两方向均在主 PG 事务内写入独立 copy outbox，原始正文不截断；独立 worker 发送授权主体本人飞书私聊，复用 `extshared.ExternalSenderHeader`。
- copy 内容在 UTF-8 字符边界截断至 20,000 字节；事件键及 Feishu request ID 稳定；失败独立退避重试。authenticated reply-parent 精确匹配已发送的 bot channel/message ID 后抑制回流。
- `DW_MCP_FEISHU_COPY_ENABLED` 默认开启，仅显式 `0` 关闭；主消息入队与副本写入错误隔离。

## 验证

| 门 | 结果 |
|---|---|
| `HUB_TEST_PG_STRICT=1 go test -p 2 -timeout 40m ./...` | PASS；全仓所有包无失败，含新增 worker 失败重试 PG 用例 |
| `HUB_TEST_PG_STRICT=1 go test -p 1 -timeout 10m ./internal/store/migrate` | PASS |
| `HUB_TEST_PG_STRICT=1 go test -p 1 -count=1 -timeout 10m ./internal/extseat` | PASS |
| 2026-11-10（完成于 10:17Z），`HUB_TEST_PG_STRICT=1 go test -p 1 -count=30 -run '^TestFeishuCopyOutboxBothDirectionsAndReplySuppressionPG$' -timeout 10m ./internal/extseat` | PASS；30/30 次，rc=0。夹具分别将 out/in 的 created_at 设为 now()-2s/now()-1s；每次投影后均断言状态为 sent，意外事件仍由替身发送器报错。 |
| `go test -p 1 -count=1 ./cmd/workpulse -run '^TestR53FeishuCopySender'` | PASS；sender 通道选择、个人收件、稳定重试 ID、不同事件 ID及无匹配/歧义/不可发送通道拒绝用例均通过，rc=0 |
| `python3 tests/mcp-p1-feishu-copy/mutations.py` | 10/10 语义判红；新增 RequestID、错误收件人与绕过 CanSend 三刀分别命中 `MCP_COPY_REQUEST_ID`、`MCP_COPY_RECIPIENT`、`MCP_COPY_CHANNEL_CAN_SEND`；编译失败未计红 |
| `PATH=/tmp/lockvenv/bin:$PATH scripts/run-build-tests.sh` | PASS；runtime-lock venv Python 3.12.3，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。首次系统 Python 调用因 helper inventory discovery 失败；锁定 venv 重跑通过。 |
| `scripts/run-build-tests.sh` | PASS；`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2` |
| OpenSpec 1.16.0 `validate --all --strict` | PASS；34/34 |
| `git diff --check` | PASS（提交前复核） |

## 变异刀

共享来源抬头、20,000 字节上限、只读与原文完整提示、反向方向标签、reply-parent 回流抑制、默认开启开关、副本入队失败不影响主事务、Feishu RequestID稳定且与事件关联、收件人为认证 Subject 而非内部目标、通道必须 CanSend：10/10 语义判红。

## 交付坐标

分支：`togpt60lunazzc/mcp-p1-feishu-copy`。

- 功能提交：`a1505c32974a4d0671567066c774357eef26b583`
- 功能提交 tree：`e10184ff078d2e31cd5caecb6d4f168e8fe66eef`
- 该功能提交已推送；推送时 `ls-remote` 与本地 SHA 一致。
