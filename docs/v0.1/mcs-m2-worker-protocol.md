# G2-M2:真 CLI 工人接入协议 v0.1(草)

件=排期 v0.3 L1-M2 的工人侧半边;线格式基线=M1 已双审闭(843fcfa)+收发人强制(85828fe);真信道实证=07-25 16:23Z 自环三连(fixture 已入仓 29a1062)。

## 角色
- 调度侧:mcsd(hubdriver.Adapter 外呼+Daemon.HandleHubReceipt 收账)。
- 工人侧:任一 DingWei CLI 会话+工具 wkr.py(/home/devo48/work/heartbeat/wkr.py,后续随客户端改造批进 sessionHelper 装机面)。

## 工人侧义务(会话提示词/角色说明书用)
1. 收到正文为 `{"kind":"mcs_dispatch_v1",...}` 的信封:
   a. 校验 to==本会话全名(错投→回执拒?v0.1=忽略并告警发件人,不伪造回执);
   b. 立即 `wkr.py ack <信封> --to <from>`(processing 回执);
   c. 执行 payload 任务(工人自身 CLI 能力);产物落持久路径;
   d. 完成 `wkr.py done <信封> --to <from> --ref <产物路径>`;失败加 `--fail`。
2. 幂等:同 rid 重复信封=重投,若已执行完只重发 done 回执,不重复执行(执行点恰一,M0 3.1)。
3. 身份:回执 from=本会话全名(wkr.py 自动:SH_FULL_NAME→在线清单自查;短名拒发)。
4. 授权边界:工人=SS 级(M0 v0.7 C-5),不持 task authority;回执只报事实(done/fail+ref),
   状态裁决在 board 闸;工人不得自称调度者身份。
5. 超时自觉:工人预估超长任务应先 ack 再执行;无 ack 的投递按 redeliver 语义重派(S6)。

## 调度侧配套(M2 实现清单)
- [x] 外呼+回执桥+daemon 挂载(M1)
- [x] 真信道往返实证(自环)
- [ ] mcs-demo 增 `--workers <全名,...>` 模式:Registry 换真会话,替换 replay 模拟件
- [ ] 面板数据源=同一 board(真账即真显,无额外改造;验证项)
- [ ] 负例:工人半死(不 ack)→redeliver;超时;乱序回执(done 先于 processing 到);
      错投信封(to≠自身);重复 rid 恰一执行

## 已知限制(诚实条款)
1. 工人执行=会话内自觉协议(提示词层),无强制沙箱;恶意工人可假报 done——board 侧 gate
   验收(tester 负例门)是真验证层,回执仅进度信号。
2. wkr.py 依赖工人机 send.py+在线清单;跨账号会话未验证。
3. 错投处理 v0.1 从简(忽略+告警);拒收回执语义留 M3。
