# RETAIN legacy 无 pin 兼容返修交件

冻结源 f3cc2b1f0136a4d698165f673fd42f33cdfc9191，tree b733a6fda31d745a131aab17f3040a6fa387f078；父 01c9c777，不改写前序。只改 guard、retention 测试与撤修登记三处产品／测试文件。

阻断关闭：select／arm 改用 if_present 事务，retain 不存在则不校验父链、不创建 retain／txn 锁、不等待锁；仅 private 注册 create=True。仍有 retain（含坏对象／软链）时全部保留原保护。None 快照在 state.lock 内重核 retain 是否出现，首次注册竞态拒绝而非漏 pin，不反转 retain→state 锁序。与 r37 旧 guard 的真实隔离对照：组写父目录及无 pin 锁忙两格均选 factory、无 retain 创建；新回归另核 arm 与 trial。LEGACY-COMPARISON.json 保留两源读数。

作者冻结源全门：vet rc0；全仓 Go rc0（53 测试包，13 无测试）；隔离 HOME＋锁定 venv helper 924 OK；canonical python61＋shell2；原49撤修＋本件40撤修（新增三格）均指定 AssertionError 红、恢复绿；官方0.161.0／0.163.0真实 owner／FD／同ID恢复／PTY门 rc0；三浏览器门 rc0。原始命令、UTC、环境键及日志在 final/；不改写前序 b75 红及01c9读数。

限域：finish 只核 stop 收据格式；认证依据客户端同 FD 挑战和锁释放。能改本席私有记录的同 UID 伪造不在该威胁边界内，不将格式检查称为独立服务端认证。未部署、未启用 private、未改稳定根或任何席位。

运行 VERIFY.py 核冻结 Git blob／SHA 和本目录全部证据。原始 unittest 行尾与 PTY CRLF 原样保留，不为了 diff-check 改写原件。
