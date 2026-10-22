# RETAIN 最终 legacy 兼容返修

最终冻结源 0407342abd31daa7ef8bc06e46c89fba26fbbc08，tree 6d26cfdf35382579492a35e1c2bdc7d9186695e2，标签 evidence/coder/pce-retain-final-6d26cfdf；父 d74f92dd，不改写已交提交。d74 只关闭 guard 的同类问题，现追加关闭 SelfUpdater 路径；01c9 与 d74 不再为最终候选。

只改 SelfUpdater.retention_transaction 为 if_present，None 时仍经 guard.check_locked 在 state 锁内重核首次注册竞态；仅 private register 的路径 create=True。select／arm、实际 Console 更新 prepare→下载→验包→stage→arm 在无 retain 时不新建 retain、不等 unused txn lock、不检查额外父链；已有 retain 则所有保护保持。旧 guard 无 RETENTION_API 的兼容分支不变。

新增两条真实 loopback HTTP 更新格：组写父目录无 pin、无 pin 但 txn 锁忙均可完成 prepare／arm，不创建保留目录；撤回 if_present 各指定红。LEGACY-PREPARE-COMPARISON.json 与 COMPARE.txt 额外从精确 r37 Git 取原 updater／guard，与后继分别完成真实下载／验包／stage／arm，四读数一致；凭据均合成、无真实 Hub 或席位。

冻结源作者全门：vet rc0；全仓 Go rc0（53 测试包，13 无测试文件；30 条既有条件测试跳过，见 GO-COUNTS）；helper env-i／隔离 HOME／锁定 venv 926 OK；canonical 61＋2；原49＋本件42撤修全部指定 AssertionError 红恢复绿（共91）；官方0.161.0／0.163.0真实 owner／FD／同ID恢复／PTY两门 rc0；三浏览器 rc0。原始件在 final/，不改写前序读数。

运行本目录 VERIFY.py 核冻结源 Git blob／大小／SHA 及本目录全部证据。历史报告的 VERIFY 须在各自锚运行，不能拿不同源工作树替代；所有旧提交／标签保留。原始 unittest 行尾与 PTY CRLF 不为格式重写。

限域：finish 只校验 stop 格式；认证靠客户端同 FD 挑战与锁释放。可任意改本席私有文件的同 UID 伪造不在该威胁边界内。guard 与新 updater 应按本候选成对集成；不把未发布的早期 RETENTION_API=1 前序当独立兼容发布。未部署、未启 private、未换稳定根、未碰任何席位。后续 CWD 安全项随下一大版集成，不单独部署；两分支 canonical 要取并集（预计62），不得覆盖已有保护。
