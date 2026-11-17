# R60①调用方回执与恢复（集成/发版通知共用）

依据组长2026-11-16T22:18:36Z令；最终①产品b48a0818，已获组长与qwen独立通过，正式集成基110e2c1f、顺序③④①②、版本0.2.3.40。本件不是部署指令。

## stdout、stderr与退出码

安装到各席`~/.dingwei/send.py`的源码为`tools/sessionhelper/sessionhelper/send_dingwei.py`。责任回执从旧的`sent to=… bytes=… request_id=…`文本行改为stdout一行JSON。不能再grep旧`sent`作成功判据；调用方须同时读取JSON与退出码，并保存request_id。

| JSON outcome | phase / accepted_by 示例 | 退出码 | 调用方动作 |
|---|---|---:|---|
| accepted | local_queued / local_helper，或hub_durable_queued / hub | 0 | 已有持久责任；local_helper只承诺本地接纳，不证明Hub已收/对端已读/模型已处理 |
| unknown | unknown / unknown | 2 | 结果未定，可能尚未进Hub，也可能已提交但回执丢失；只查询或复用原ID，不按正文新建请求 |
| definitive_failure | rejected / none | 1 | 明确拒绝，读取reason；不要无条件循环重跑 |

以下为结构样例，不是现网回执。可选字段会因阶段不同增减；reason不应按中文展示文本解析。

accepted（本地接纳，rc=0）：
```json
{"outcome":"accepted","phase":"local_queued","accepted_by":"local_helper","request_id":"r60-notice-001","source_message_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","status_ref":"r60-notice-001","retryable":false}
```

unknown（rc=2）：
```json
{"outcome":"unknown","phase":"unknown","accepted_by":"unknown","request_id":"r60-notice-001","status_ref":"r60-notice-001","reason":"ipc_confirmation_unavailable","retryable":false}
```

rejected（outcome为definitive_failure，rc=1）：
```json
{"outcome":"definitive_failure","phase":"rejected","accepted_by":"none","request_id":"r60-notice-001","reason":"permission_denied","retryable":false}
```

常规明确失败另向stderr打印`发送失败：<reason>`，不是旧的仅中文成功/失败接口。参数用法、空正文、不支持的子席目标等本地错误也可直接以文本退出1；输入文件或调用方账本拒绝等提前失败路径可只有JSON。不要假设所有rc=1都带request_id或同一种stderr格式。

## 原样命令与rc=2纪律

已逐行核源码main参数解析和ipc_send：支持`--status <request_id>`、`--request-id <原值>`，也支持`--file`。建议首次发送前自行生成并保存稳定ID，正文文件在恢复期间保持原样。下列是命令格式，不是让各席立即发真实消息。

```sh
request_id="r60-notice-001"
target="目标席实际在线全名"
body_file="/absolute/path/original-message.txt"
python3 "$HOME/.dingwei/send.py" "$target" --request-id "$request_id" --file "$body_file"
```

上条若返回2，不得再用省略`--request-id`的同正文命令。先查询，查询本身不发送新正文：

```sh
python3 "$HOME/.dingwei/send.py" --status "$request_id"
```

需要恢复原请求时，保持原sender身份、原目标、原正文和原ID：

```sh
python3 "$HOME/.dingwei/send.py" "$target" --request-id "$request_id" --file "$body_file"
```

同ID同内容会恢复同一helper账本/source_message_id及Hub原delivery_id；同ID不同内容拒绝为冲突，不能据此改ID绕过。`retryable:false`不允许换新ID自动补发；这里的同ID恢复是责任协议专用操作，不能推广成对其它unknown操作的通用重试许可。保持helper私有账本，不删库/换席/换通路重发。超出恢复窗口或身份变化时保留unknown并人工核对；不声称任意久远重放都永久去重。①默认helper自动恢复窗24小时、Hub默认栅栏7天且启动校验大于26小时；具体边界仍按①设计及部署实际配置。不要用新的正文请求“补偿”未明结果。

## 上线后文档与引导面待更新

以下是组长22:18:36Z提供的板上旧输出引用定位，本席未独立遍历整板，行号上线前须按板件冻结tip重核：

- `tasks/v0.1-B`：`runbooks/ACTIVE-ASSIGNMENTS.md:1296`。
- `tasks/v0.1-B`：`HANDOFF-qwen38mvb4-20261113-HELPER-UPGRADE.md:61`。
- 同板其它引用`sent to=…`/“sent即到达”的件，由板件owner上线后逐项检索更新，不将这两条冒充完整清单。

本席不写该板，不改各席skill/系统提示。组长在上线通知中告知各席新JSON/退出码/原ID恢复命令，并安排板件owner更新；通知不能先于实际helper升级生效而让旧版客户端照用新参数。

## 正式集成回归

TestHostedBodySendSameRequestIDAcrossCLIProcesses在两个独立进程中运行生产installer安装的send.py，经真实Unix IPC、helper/outbox、loopback WS与Hub SQLite；身份为合成MemStore，无模型/在线席位。使用同一--request-id，显式--status后确认同source/delivery，实际created=true只一次。CLI走标准完整Agent IPC；原MCP格仍保留受限body-only入口，不为CLI放宽权限。删除原ID复用保护须业务红。

TestR60OutboundControlBackpressureRecoversWithSendStatus另测完整①②：持有真实Hub source写槽，生产2秒截止导致已提交回执写失败，失败计数增加后才经真实MCP dw_send_status观察unknown；helper仍保留原正文/ID。释放写槽后，自动writer在原10秒回执等待后按原source恢复，dw_send_status观察原delivery，Hub创建恰一次。查询本身不重发、不执行模型；网络允许幂等重送，不能把一条Hub责任说成只有一个网络帧。删2秒截止或同source复用须业务红。显式DW_DESKTOP_INTEGRATION_PYTHON指向隔离解释器，正式门不接受这两格SKIP。

工作态首轮编译失败（多余import）、联合格业务断言已过但子进程结束标记缺失的第二轮失败均保留，修正仅测试夹具。定向最终绿不代替固定源全门。旧本地预演证据不计正式读数。
