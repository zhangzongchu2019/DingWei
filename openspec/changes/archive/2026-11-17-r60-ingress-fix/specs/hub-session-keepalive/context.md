# 兼容修订基线

本变更把旧helper可正常发送的小帧突发从64项断连改为已裁定的有限兼容窗口，目的是保持控制帧读取、可靠处理顺序与预算边界。涉及Hub helper WS、队列和写截止，不影响身份授权、0077或发布批准。

现有能力基线：9818b979的openspec/changes/hub-keepalive-fix（HKF-001～009）、r60-integration及helper-send-lost；观察源分析分支8f0d5ce4/方案0d8e2539。旧39源2e1e4c9d，r57对照329eb9f9。未复原五次生产关闭的逐笔原因；缺既有helper外部stdout与monitor快照，不补造。

## 当前验证状态（a24bb1633）

实现与局部回归/两批删保护完成，完整门尚未通过：m8串行第四轮在允许两例之外的TestSessionNameReconnectKeepsNameAndReleaseReuses（hub_test.go:3505→waitEndpointInactive:6551）出现interrupted(9)，已按组长01:37:18Z门停止，第5轮与后续门未跑。完整四轮与canonical失败/重跑见docs/reviews/r60-ingress-fix/REPORT.md及stopped目录；等待组长处置，不引用本件作上线依据。

## 2026-11-17T21:00:33Z门修订

组长据togpt60lunazzc/z3-new-site 43ec0a67f0ef7862139d4831b69c2a511a90b4ee的基线复现记录，明确将3505落点加入本次名单。当前只准三格，逐名见design.md；固定a24bb163全新连续串行五轮、至少三轮整包rc0/0 FAIL，不沿用此前四轮，不自行新增例外。其它门已在docs证据5fa77cff0记录：Go其它树1033 PASS/12 SKIP、focused race79/0/0、联合/双CLI逐名2 PASS/0 SKIP、浏览器首轮环境失败保留/同源全量重跑45/45、strict41/0。本次新五轮单独记录，未完成前不称全件门通过；不执行预发或生产动作。

## 2026-11-17T21:30:50.688962+00:00 新五轮完成

固定a24bb163全新连续串行五轮5/5整包全绿，按21:00:33Z裁定达到作者门，详情及逐轮原日志见REPORT文末/z3-revised-five；未沿用旧四轮，未改产品/store/测试，未新增例外。其它门同源已齐，提交组长复核；不是独立PASS或上线依据，预发计划仍未执行。
