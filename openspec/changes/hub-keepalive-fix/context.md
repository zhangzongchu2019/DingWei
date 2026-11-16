# S-HUB-KEEPALIVE-FIX上下文

- owner：coder56solzzc.zzc；组长2026-11-16T14:57:50Z只读派单，期限20:00Z。只分析＋ADDED提案，不改产品/现网，不用截图代替现场逐连接trace。
- 分支coder56solzzc/hub-keepalive-fix，基main d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e；监控源锚329eb9f9c45d2d785a0429b01eaa0fa5264fe15d。Hub源码相关路径三锚329/2e/d2逐字相同，helper app/IPC相同；driver_adapter只有2e/d2相同，329内版本不同，逐文件比对见manifest；不从作者推导部署。
- 先读openspec/specs/hub-lifecycle/context.md、spec.md与helper-delivery-evidence/context.md、spec.md，再读进行中hub-graceful-shutdown；保持HLC关停归属和ACK持久边界。没有现成独立keepalive能力主spec，因此在本变更specs下补最小context；平台能力时间线专件未定位，不补造。
- 测试普通uid1001，独立HOME/TMPDIR、锁依赖、env白名单，Go overlay只注入测试文件，不改Hub产品。Go overlay不存在虚拟测试文件时vet报路径缺失，首轮rc1保留；重跑只对该分析probe用-vet=off，非豁免产品vet。完整canonical另行运行。
- 交件报告放docs/reviews/hub-keepalive-fix-20261116/REPORT.md，不占仓库根REPORT；S-SEND-LOST保持aaa3f893上的冻结设计字节，S-FLAP停60e9a80c。

## 2026-11-16 R60 实施中

- 授权：组长2026-11-16T16:05:24Z派R60②；业主16:03:17Z同意r60（㊿-补123 2f5560b7）。本分支coder56solzzc/r60-hub-keepalive，基main d2cc47c7；不含①send-lost或③④，集成另在110e2c1f基点按③④①②办理，版本只在集成升0.2.3.40。
- 冻结提案9eeea527，实施候选尚未固定/推送；唯一Reader、有界顺序队列/全Hub字节预算、合并touch、viewer独立输出责任、总写截止、历史等待取消、保活失败journal、helper文本分块已落初稿。真实慢业务6.1秒/健康输出对照、viewer锁等待、ACK提交顺序/关停与超载初测通过；不冒称固定源码完整门或独立PASS。
- 非目标：不部署/改生产配置，不修故障接管/上下文共享，不顺手修既存ACK测试flaky。独立复核交qwen，终审交组长；本席不自验收。
- 环境：本机overlay耗尽影响①校验（原rc1保留）；已将本席旧Go缓存与运行产物迁数据盘，保留原路径软链接，未动他席数据。②临时件在本席local-run-artifacts目录，不进入源码。
