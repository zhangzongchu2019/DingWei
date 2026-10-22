# PCE 运行时保留：最小首期实现合同

基 `c6851ed9`；设计 `7d0756b8` PASS 后按组长收窄的①至④实现。本件不部署、不提供稳定 launcher 迁移执行单，也不启用 private。默认 legacy 未变。

实现位置：保留核内嵌于现有稳定 `update_launcher/guard.py`，能力 `RETENTION_API=1`；发行包根白名单没有新增成员。helper 从显式已核 `SH_UPDATE_LAUNCHER` 加载稳定核，旧 launcher／未配置／能力不符均拒 private；普通无 pin 的 legacy 升级、试用、回滚保持原合同。稳定文件必须由后续另批的安装维护更新，源码 offer 只提取 factory，不替换挂载稳定核。

在 `<state>/update/retain` 注册 durable active pin 与三个共享锁（本 pin、包、运行时）；fsync 后才 Popen，将实际 FD 通过 pass_fds 交接。owner 在同一 inode 上重新取得 SH、核 pin／源码／解释器及端点绑定；helper 关闭自己的 FD 后，owner 仍持锁。child close_fds，不继承这些锁。helper 重启不解除 pin；child 异常、owner 失联或交接未确认，active pin 与 unknown 同样保留，锁消失不是回收授权。

同摘要碰撞替换／rmtree 检查 pin；prepare 在 retain.txn.lock→state.lock 下重核 pending、保留和候选，再 rename。网络下载不持这两把锁。guard 的 arm、trial/current/previous/factory 选择都过保留门；注册新 owner 也核 pending，封闭反向并发窗口。pin 的包必须仍为原 inode／摘要，源码候选整个 sessionhelper Python 闭包、解释器调用路径／实际 exe、prefix/base_prefix 身份及锁版本任一变化即 manual_required；不实现兼容摘要放宽，版本元数据变化亦可能被保守拒绝。

运行包始终原位保留；源更新继续锁字节匹配且不安装运行时依赖。解释器（含 venv）和系统标准库不作为本期回收对象，固定基线不得原位改动；共享运行时锁只约束受控操作，不声称能阻止管理员绕过流程 apt／pip 或直接改文件。运行时维护须在所有相关 owner 按门退出后另批，不复制凭据或环境。

仅经原有同 FD owner 身份＋随机 challenge 的成功空闲 stop 响应，确认 pin／nonce 并观察 owner.lock 释放后，稳定核才提交 stopped 回执；owner 先释放继承的 runtime 锁，再释放 owner.lock。响应丢失、超时或写证失败留 active，不补造证。显式入口为稳定 guard 的 `--retain-reclaim <32hex pin-id>`：仅收已核 stopped 的单一 managed slot，仍须核 current／previous／pending、其他 active/unknown 引用、独占包锁及删除前最后 inode。factory、外部包根、解释器和 venv 均不删除；不按 PID 文件接管／杀孤儿，不后台 GC。

坏 pin、权限／软链／inode 漂移、3秒事务锁超时、存储失败均拒绝并保留；128条 pin 上限含回收审计，达到上限阻止新注册，不自动抹历史。未知 owner 的恢复单与审计归档留后续。完整验证包括独立隔离子进程的实际 owner 崩溃（child 仍活、pin 拒收，夹具精确清理收尸）、FD 交接、注册／回收竞态、候选／回滚／碰撞与精确回收各门撤修，另跑 full helper、canonical、全仓 Go/vet、两版锁定 Codex 原生门及相关浏览器。通过作者门仍待组长审与 prodops 交叉核，启用仍须另令。

补强：owner 以 -I -B 与显式已pin包根trampoline启动，隔离业务cwd及用户Python路径，避免先执行同名项目包／标准库模块。stopped/reclaimed记录须完整nonce绑定回执才可读；所有未显式回收的pin（含stopped）均拒同目录碰撞替换。旧launcher仅在retain不存在或经nofollow／0700核为全空时维持legacy原流程，任何未知条目均manual_required，private仍必须新能力。
当稳定 launcher 提供 SH_UPDATE_ROOT 时，PCE pin 根与自升级 cfg 根须均与其相同，不一致即拒，避免把 pin 写到自升级器看不到的位置；不引入新环境键，也不改变无该绑定的隔离直接API夹具。
