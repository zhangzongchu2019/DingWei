# 任务

- [x] T-01 定义 FEISHU-COPY PG 持久化 outbox、0111 迁移及幂等状态转换。
- [x] T-02 在外部→内部和内部→外部入队事务中原子创建抄送事件。
- [x] T-03 接入授权人本人身份映射与 Feishu Gateway，复用 `extshared.ExternalSenderHeader`。
- [x] T-04 实现只在抄送 payload 截断、默认开启开关、失败隔离、重试及回流抑制。
- [x] T-05 增加双向真实 PG 入队/投影、bot-channel 回流边界、幂等、失败隔离、开关及 7 项目标 mutations 刀。
- [x] T-06 更新主规格与 context，执行全仓门、canonical 门和 OpenSpec strict，记录 RESULT。

## 交接记录

2026-11-10：`togpt60lunazzc.zzc` 按 rulings ㊿-补65 切换至 r56.4 集成 `af752f641`，PG migration 分配为 0111；复用既有 `extshared.ExternalSenderHeader`，实现双向事务内抄送入队、PG worker、Feishu 私聊发送、20,000 UTF-8 字节副本截断、默认开启开关和 reply-parent 回流抑制。7/7 semantic mutations 红；全仓、canonical 和 OpenSpec strict 均通过；提交前。

- 完成：主消息失败隔离已有真实 PG 测试；复制内容与回流抑制定向测试通过。
- 完成：0111 迁移、真实 PG 双方向投影与失败重试测试已补；全仓 Go、canonical build、OpenSpec strict、7/7 semantic mutations 均通过。
- 阻塞：无。
- 下一步：复核最终差异，单提交推送并核对 `ls-remote`。
