# 兼容修订基线

本变更把旧helper可正常发送的小帧突发从64项断连改为已裁定的有限兼容窗口，目的是保持控制帧读取、可靠处理顺序与预算边界。涉及Hub helper WS、队列和写截止，不影响身份授权、0077或发布批准。

现有能力基线：9818b979的openspec/changes/hub-keepalive-fix（HKF-001～009）、r60-integration及helper-send-lost；观察源分析分支8f0d5ce4/方案0d8e2539。旧39源2e1e4c9d，r57对照329eb9f9。未复原五次生产关闭的逐笔原因；缺既有helper外部stdout与monitor快照，不补造。
