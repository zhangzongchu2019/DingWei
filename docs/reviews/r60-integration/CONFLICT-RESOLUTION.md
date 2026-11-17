# R60 正式集成与冲突解决

本树基110e2c1f63b1530d1ca05b92cd723312ef16c0f8，保留现网图标热修；按组长2026-11-16T16:05:24Z、20:57:01Z令及四项冻结条件，正式按③④①②合入。版本统一0.2.3.40，无main推送/预发/部署。

| 顺序 | 冻结产品 | 实际合入证据tip | 本地merge |
|---|---|---|---|
| ③ MCP两工具 | b4c42adb75c7b9cb1847dcc97fdc856e51b7f42f | 899f4328bc418edff80e06fa49ab3a5471f8a9ef | 7ee4c149433b62f6c8efa2c39d6c8a06571929d5 |
| ④ 错卡重发 | 4e421abea6399c1de78040e14e4a736d97340671 | 同产品 | 70bf89904388c2b63f0731e6fb9a4628d41b40ff |
| ① 发信持久责任 | b48a0818aa121a3f68b921fac489926257ad841d | 81a887308688a545cc0edef39559c2e7120e2340 | 9290d63e99177ebdbec5c2fed7cc2cc571fdae70 |
| ② Hub保活 | 3db78e5b244c4663fe67f7dcd44b74991057a7f6 | b17593fbe8563cf1cff6ee4db1b9d4727b53ced5 | 6f7dcad70d1b3aa308f38a73432dce6c8397f045 |

③④及②已由组长冻结；①组长00:29:40Z通过，以qwen对b48复核通过为冻结条件，该独立spec/code PASS于02:2xZ收到。qwen报告aa41c48e的10个日志在初查Git中缺失，已请求报告方追加，不把两篇文档SHA核对冒称原始日志核验。

## 两处文本冲突

合①时app.py保留current分派，heartbeat_handler=current(self._make_heartbeat_handler)，依当前WS能力头判断；保留①desktop当前WS与非desktop本地inbox分支。deploy/test_build_entrypoints.py按实际inventory97（基94＋①1＋③2）。②自动合并，无新增文本冲突。真实持久Unix IPC重连回归验证新连接发送、旧连接不再发送及无能力返回unsupported；删除current包装须业务红。

本地预演0cf80ce0/b7ba6ca5只贡献测试准备，不以其旧①12f/②167的门读数代替本源。正式新增仅版本钉住、冲突回归、真实双CLI同ID、①②联合格、Z1两格及文档；冻结分支不改。正式门与独立读数分别留证。
