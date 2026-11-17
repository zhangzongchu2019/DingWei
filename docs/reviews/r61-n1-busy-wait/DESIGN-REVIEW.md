# S-R61-N1 设计交件

基点：R60 集成锚 `9818b979e9e20f0bcfeffa6b78045dda71b1bd79`。

本交件仅新增 `openspec/changes/r61-n1-busy-wait/` 设计文件；无产品代码改动。设计覆盖派单 1～6 项。需组长先决定的实施阻断：ACK v2 目前不能表达 `queue_full` 终态拒收，且 `busy` 会延后再投；要满足发信方可见拒收，需 Hub 协议/通知协作。此处只记录需求，不改 `internal/m8`。

OpenSpec strict 尚未运行；本环境未发现 openspec CLI，因此本交件不声称 strict 已通过。
