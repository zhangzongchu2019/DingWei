# Proposal

## Why

现有 helper 的普通主动发信在已协商回执路径中，把发送前异常、断线、写入结果不确定和 Hub 回执超时合并为 unknown，并一律禁止重试；普通 send.py 不持久保存待发正文，结果可既未进 Hub 也没有自动恢复负责人。两条已报告 source_message_id 在 Hub 快照和日志中均无原始记录。需要分开发送前明确失败与可能已提交的未知，建立本地持久责任和 Hub 重放幂等，避免只把 retryable 改为 true 后制造重复业务请求。

## What Changes

- 提案新增 helper-outbound-recovery 能力，8条增量要求，定义发送责任、稳定意图、本地持久队列、Hub原子幂等、连接代际、结果语义、受控恢复与取证。
- 推荐本地持久队列与Hub持久幂等作为同一发布单元；短期无持久责任时，只有可证尚未接管/未写出的失败才返回可重试拒绝，不对已可能提交的 unknown 开盲重发。
- 原只读提案已由R60实现派单接续：①本分支仅实现普通主动发信恢复；新增本地SQLite账本与Hub迁移0077。不apply/archive、不接触现网，交审后由组长终审。

## Capabilities

### New Capabilities

- `helper-outbound-recovery`：普通根Agent主动发送，从易失内存等待改为可查询、可恢复的持久意图和唯一Hub入队。最小现状见同目录context.md，拟议合同不是已实现功能。

### Modified Capabilities

无。helper-delivery-evidence中的收件模型入场unknown不重放继续成立；本提案只恢复“helper→Hub收信”提交，不重新调用目标模型或强制重做其业务。

## Impact

后续实现预计涉及send_dingwei.py、mcp_shim.py、ipc_listener.py、app.py、outbound_receipt.py及Hub入队/回执/状态查询。需本地私有SQLite发送账本与Hub持久幂等关系；具体DDL、迁移号、性能与发布集合另行评审，本单不申请占号。既有飞书、外部席位、子Agent和desktop专用回信有各自协议，不默认纳入自动重送范围。需要兼容协商，不能向旧Hub宣称支持重放。

## 来源

需求来源：[S-SEND-LOST只读取证](https://git.code.tencent.com/wego-ai/dingwei/blob/91f1b3adcc3ed60ce25ff24fcb8f7c727b909d86/docs/reviews/prodops-flap-two-missing-20261116/RESULT.md)；组长opus50tc2k.zzc在本会话2026-11-16T09:06:48Z派单S-SEND-LOST，要求只读回答A～E及ADDED提案。派单delivery_id未提供，不补造，不从提交作者推断授权。分支基main d2cc47c7；审计helper固定2e1e4c9d（0.2.3.39）。

## 待确认假设

- SL-A01：所有需要自动恢复的普通根Agent发送都能取得稳定发送Agent ID和明确目标Agent ID；不能只凭可能改名/替换的短名恢复。
- SL-A02：helper私有持久卷可在重启后保留发送账本且有可监测容量；容器销毁/盘丢失不在本地持久性承诺内。
- SL-A03：当前7天helper侧发信元数据和部署指纹保存情况未取得；不能从Hub单侧估算完整丢失总数。

## 待 PM 决策

| 编号 | 选项 | 建议与约束 |
| --- | --- | --- |
| SL-PM01 | 只做明确未提交失败；或本地队列＋Hub持久幂等 | 推荐后者；只改retryable不能覆盖写入后回执丢失 |
| SL-PM02 | 首批普通根Agent同组可靠会话；或同时覆盖飞书/外部/子席 | 推荐先限普通根Agent单目标、无正文AMR扇出的可靠会话，其他路径显式不支持自动恢复 |
| SL-PM03 | 容量/期限/保留窗 | 候选每席4096条与32MiB待发正文、重放期限24小时、Hub栅栏至少7天且大于重放窗；均待容量评估，不是上线定值。超限拒绝新增，不淘汰未确认正文/栅栏 |
| SL-PM04 | 未接管/明确拒绝用非零，unknown用独立非零，local_queued是否正常返回 | 建议CLI区分四类且输出结构化ID；local_queued可正常返回但必须明说尚未进Hub。退出码具体数字与兼容脚本另定 |

## R60 实施授权与当前边界

2026-11-16T16:05:24Z组长派单R60，依据业主16:03:17Z“R60 --- OK，可发”（㊿-补123，2f5560b7）；①基main d2cc47c7独立实现。历史待PM表保留为提案原态，当前实现候选容量/退出码与首批协议边界在design的R60节明列，最终上线定值随独立评审及组长终审确定。无生产操作、无旧unknown导入或批量重发。
