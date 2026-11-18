# 插桩诊断副本（取证用，不入库运行路径）

两件均在基锚工作树 /tmp/wt-anchor-501/view-service/ 内以未跟踪件运行（相对 require 与 GO_SOURCE 指向基锚产品件），本目录仅存档供复核重放：

- console-instrumented-followup.js：test() 失败时转储全页 DOM/产品旋钮/longtask＋网络时序＋截图（EVIDENCE_TAG 命名）。
- console-specs-instrumented-followup.js：S-7 段 try/catch 转储 clickedRowConnected/rowStatus/网络 marks；EVIDENCE_LIST_DELAY_MS 为**定点武装**延迟（/__dw_arm_list_delay 标记后才延迟列表 GET，仅用于 S-7 机制演示；首轮全局延迟误伤前段的 diag-01/02 已作废为诊断伪影）。

重放示例（基锚树内）：
EVIDENCE_TAG=x NODE_PATH=… node view-service/console-instrumented-followup.js --case='distinguishes platform'
EVIDENCE_LIST_DELAY_MS=350 EVIDENCE_TAG=y NODE_PATH=… node view-service/console-specs-instrumented-followup.js
- specs-fixed-instrumented-diag.js：**修后版**同形插桩（S-7 转储＋定点武装延迟），用于机制阳性对照（基锚 3/3 红 vs 修后到达即绿）。运行时置于 /tmp/wt-followup/view-service/（相对 require），毕后归档于此。
