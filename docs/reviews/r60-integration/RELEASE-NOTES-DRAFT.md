# R60 0.2.3.40 发版说明草稿

尚未发布。四项已按③④①②本地正式集成，来源见CONFLICT-RESOLUTION.md；固定源完整门与终审未齐，不构成预发/部署/上线令。

- ①发信：helper持久outbox、Hub稳定ID幂等、原回执恢复。send.py由sent文本改为一行JSON，accepted/unknown/definitive_failure退出码0/2/1。0可能仅本地持久接纳；2禁止省略原ID重跑正文。原样命令/样例/旧板件引用待更新清单见SEND-CALLER-CONTRACT.md，组长上线通知告知各席，引导面由其owner更新。
- ②保活：单Reader与有界顺序worker、viewer总写截止与隔离、保活journal和可校验配置。离线legacy仍收到readonly状态及正常1000关闭；两秒控制写可能增加unknown，独立obs记录失败，由①原ID查询恢复兜底。helper UTF8分块在已测合成突发降低控制延迟，但不限速大正文吞吐下降，不承诺生产SLO或解释历史419次。
- ③MCP两工具：心跳/投递查询明确helper_rejected。集成心跳跟随当前WS代际与能力，避免重连使用旧连接。
- ④View：错卡修正，VRW-PM01关闭copy重发；保留基点现网图标热修retainCanonicalTrustedProvenance及其回归。

## 上线后观测

caller request与outbox outbound_request总行数/各state/文件大小；Hub栅栏窗内W、窗外未终结P与待清E/最老年龄。过期仍local_queued/unknown不清，其他非明确可回收终态保守累积，物理盘无硬界；不把窗内quota称磁盘上限。保留期外未终结Hub栅栏不计配额但不清，受理清理会经索引重扫。此为观测要求，不声称新增自动采集或管理清理接口。

## SQLite Z3

组长2026-11-17T01:37:18Z新门替代00:58Z旧门：固定源m8串行5轮逐次逐名保留interrupted(9)，只限FeishuOnce/Flapping和ViewMentionRoute指定子格，至少3轮整包全绿；越界、R60改动文件用例FAIL或不足3绿即停。不以单跑替代，不把FAIL改SKIP。togpt a931报告与qwen对照指向增量前亦有低频现象，根因仍未定，转r61，不并入本版store/驱动修法。Z1两格四刀列为本次集成必做，Z2全局obs差值仅遇实际非+1再处理。

预发/现网执行者、备份、配置、回退件与业主上线时机等组长终审和另令，当前不补造。
