# S-SEND-LOST上下文

## 当前状态覆盖（2026-11-17T00:17:15.129861+00:00）

本次交审产品/测试固定 b48a0818aa121a3f68b921fac489926257ad841d，tree567f180571e352684e06eb3966c150f8e2b48a19，parent1d8b03a6d386f359a23398da34649007d53c1aa2；main基d2cc47c7。组长23:15:43Z已撤12f冻结，以下旧12f接受及167保活输入都只属历史，不能指导当前集成。①新源独立复核和组长终审待回，②以3db78e5b另待qwen增量，正式集成仍110e2c1f/.39，未合入。

新caller维护在send_dingwei.py；Config启动解析保留期，runtime先登记实际outbox再启IPC/writer；MCP两类caller拒绝不再变-32602。新源证据docs/reviews/r60-send-lost/evidence-caller-retention；VERIFY同时核Git对象/当前产品/压缩原日志。旧evidence/evidence-retention与HISTORY按旧锚保留。最终canonical/完整四树Go/focused race/build/strict及55业务变异已跑，详细计数与命令见REPORT/summary；没有本源浏览器或整包race声明。集成必须另补完整①②联合故障格及两真实CLI幂等，不能复用旧预合当本源验收。


## 输入与边界

- 派单：opus50tc2k.zzc，2026-11-16T09:06:48Z，S-SEND-LOST；期限13:00Z；本次只读、REPORT＋ADDED提案，不实现、不接触现网。
- 分支：coder56solzzc/send-lost-20261116，基main d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e。S-FLAP停在60e9a80c179432bce15695522d0b6767064927c9，不再追加。
- helper审计：2e1e4c9db3c42cf7c987a548fa9214aad67a9f0c（0.2.3.39）；线上具体helper文件指纹与Hub二进制SHA未取得，不由版本串代替字节证据。
- prodops只读原件：91f1b3adcc3ed60ce25ff24fcb8f7c727b909d86/tree b4e4c05da6c878f5342afb7eb4f3530eddb8899a；路径docs/reviews/prodops-flap-two-missing-20261116，原四项SHA已独立核。

## 读取顺序与既有合同

先读openspec/specs/helper-delivery-evidence/context.md与spec.md，保留HCD/HRI/HBA收件未知不重放；再核进行中变更与openspec/config.yaml。主规格没有普通主动发信恢复能力，最小原态在specs/helper-outbound-recovery/context.md；现有Hub生命周期、外部sender和desktop回信并不自动为普通mcp_send提供持久outbox。平台时间线专件未定位到，不补造。

## 验证

Node22.23.1，OpenSpec1.16.0，OPENSPEC_TELEMETRY=0。普通uid1001、短独立HOME/TMPDIR、剥继承SH/DINGWEI/DW/DATAD；仅临时库和loopback，无在线发信测试。canonical在本分支基d2产品字节运行；离线当前行为探针明确调用固定2e代码。raw与元数据见docs/reviews/send-lost-20261116；不是拟议修复通过证据。

## R60①接续（2026-11-16）

- 新分支coder56solzzc/r60-send-lost，基d2cc47c7；冻结提案输入aaa3f893，R60派单16:05:24Z。旧提案与旧门不是本实现通过证据。
- Hub：internal/model/outbound_intent.go；internal/store/outbound_intent.go与migrations/0077_outbound_intent.sql；internal/m8/outbound_recovery.go及outbound_receipt.go/hub.go/sub_agent.go接线。
- helper：outbound_queue.py为私有账本，outbound_recovery.py为单writer，app.py将IPC与WS代际解耦；send_dingwei.py/mcp_shim.py/ipc_listener.py透传并持久固定caller ID和查询结果；outbound_receipt.py协商新恢复能力。
- 新增测试：outbound_intent_test.go、outbound_recovery_test.go、test_outbound_queue.py（登记helper-test-files.txt）；既有传输/地址/托管夹具改为明确责任回执，不以sent:true伪造受理。
- ①不提前抬VERSION；集成③④①②后由r60-integration统一抬0.2.3.40。②新分支r60-hub-keepalive基d2；集成分支基110e2c1f，须保留retainCanonicalTrustedProvenance。


### 2026-11-16组长容量退回当前状态
旧6163fbb4/ba7acae7停止交审，累计永存栅栏方案被退回。本轮只修时间窗保留/有界GC及配置/0077索引、物理身份绑定保护和回归；默认168h、启动>26h、容量仅窗内计。新冻结产品锚及完整门尚待生成，旧25刀/旧绿门不得冒称覆盖新实现。最终再给qwen一次新tip；③优先、②产品167不受影响。

2026-11-16T23:15:43Z组长撤销12f冻结：caller历史容量、MCP错误折叠、retention上界需改。新代码入口send_dingwei.py caller注册/created迁移/保护GC与异常类型、mcp_shim专门捕获、config/app启动配置登记、outbound_queue/recovery同类终态历史清理、store/cmd上下界；不改0077/VERSION。原门不外推新源；当前工作态定向绿，固定新源完整门/全刀及新独立复核待完成。
