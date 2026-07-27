# C-P0 集成自测收口报告(C-P0-5;devo48 执笔,prodops 组内审)

细案=docs/c-p0-plan-v1.md@fc3a9a1(prodops AGREE sha8d40da81)。契约=interface-contracts-v1(tag^{commit}=107015744b0e56c9645c84eb6ed97bf479c7d918)§2.2/§2.3。

## 一、交付锚(两仓线)

| 步 | 交付 | 锚 | 组内审 |
|---|---|---|---|
| C-P0-1 契约面 | internal/bridge/bridge.go+映射文档+BR骨架 | impl/p3-prep **d56cf15d** | prodops AGREE✓ |
| C-P0-2 WS-driver解耦(hub侧) | internal/m8/driver_health.go+hub.go 三改 | wip/c-p0-bridge **5b704481** | prodops AGREE✓ |
| C-P0-3a StubHub | internal/bridge/stubhub.go+BR测试 | impl/p3-prep **ea38427** | claudek3 复核严丝合缝(A组消费方) |
| C-P0-3b hub独立起停 | shutdown.go+cmd/mcshub+scripts/mcshub.sh | wip/c-p0-bridge **95559fb7** | prodops AGREE✓ |
| C-P0-4 Unlocker | internal/bridge/unlock.go(mock+生产骨架恒拒) | impl/p3-prep 本commit | 随本报告审 |

## 二、BR-01~14 判据矩阵(全部可执行,当刻实跑)

| BR | 判据要点 | kit 侧测试(impl/p3-prep) | hub 侧真实现(wip/c-p0-bridge) |
|---|---|---|---|
| 01 | close 带 reason 入流 | TestBR01 ✅ | shutdown_test(going-away+reason=shutdown)✅ |
| 02 | health unknown 不关 WS | TestBR02 ✅ | driver_health_test 表项3 ✅ |
| 03 | ack 链 injected→processed | TestBR03 ✅ | (E2E 归 A组夹具,接口面已冻) |
| 04 | 错误五类+Retryable | TestBR04 ✅ | — (kit 域) |
| 05 | Seq 单调+Ack 不重放 | TestBR05 ✅ | (同上) |
| 06 | 未 ack 重放恰一 | TestBR06 ✅ | (同上) |
| 07 | Interrupt 目标判定 | TestBR07 ✅ | hub V2 terminal_interrupt 既有(main a5f7c25) |
| 08 | Retryable 判定表 | TestBR08 ✅ | — (kit 域) |
| 09 | WS 断≠driver 死 | TestBR09 ✅ | driver_health_test(ok 不关)✅ |
| 10 | health 三态/unknown 禁判死 | TestBR10 ✅ | driver_health_test 归一逻辑 ✅ |
| 11 | idle 仅 degraded 触发 | TestBR11 ✅ | driver_health_test 表项5/6 ✅ |
| 12 | unlock 登记制 fail-closed | TestBR12×3 ✅ | 生产骨架恒拒(P5 前不放行)✅ |
| 13 | ack 链三段可断言 | TestBR13 ✅ | (E2E 归 A组夹具) |
| 14 | 五故障确定可重放 | TestBR14 ✅ | — (kit 域) |

另:脚本化起停验收=scripts/mcshub.sh 全生命周期实跑绿(start→healthz→restart→stop→幂等 stop,日志见"有序关停"行)。

## 三、conformance 现状(真实现与 stub 同套测试)

- hub 是 WS 服务而非 Go 接口体,同套测试经两条腿走:①语义半边=hub 单测逐条对齐 BR 判据(上表右列);②全链 E2E=A组 helper 认知端桩+E门夹具(分工已定:hub 侧桩=C组 StubHub/helper 侧桩=A组自供,互为 owner 复用,不重叠不复制)。
- A 组桩若发 session_capabilities{driver_health_v1}+driver_health 帧即可覆盖契约态三分支;不发自动落兼容态。

## 四、诚实边界(遗留,均有归属)

1. **helper 真发 driver_health=客户端改造批**(待立项):此前生产全走兼容态,wip/c-p0-bridge 上线零行为变化(为 helper 升级铺接收面)。
2. **FeishuUnlocker 真接线=P5 授权域**(业主授权原子共切):骨架恒拒 fail-closed,绝不静默放行。
3. **wip/c-p0-bridge 上线**另走发版纪律(coordinator 归口四要素+张宗楚硬 gate),本报告只收开发+自测,不含部署。
4. BR-03/05/06/13 的真 hub E2E 腿随 A组夹具落地后补跑(接口已冻无风险,进 A组验收线)。

## 五、结论

C-P0-1~5 开发与组内自测全绿收口;C 组 P0(飞书桥接+hub 拆分)开发阶段完成,转入:①A组 E2E 对接 ②客户端改造批(helper 信令)与 P5(真 unlock)两个上游立项 ③发版窗口(prodops/coordinator 归口)。
