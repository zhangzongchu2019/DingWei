# S-PCE-UPDATER-RETAIN：活 owner 运行时保留（一页设计）

**输入与边界。** 基 `c6851ed97bf0c5154f2beafbb090802942c62ad0`（PCE 一期产品已审、默认 legacy 补格通过，待 prodops 交叉核）；本件仅设计，未解除 private 启用前置。当前 `self_update.py:244–250` 只保护 current／previous，同 SHA 目录碰撞恢复会 rmtree；`update_launcher/guard.py:159` 只轮转指针，没有通用槽位 GC。共享解释器由稳定 guard 的 sys.executable 启动，更新包不带 venv，runtime lock_changed 已拒绝。故不虚构“现有自动清理”：先守住碰撞替换与所有受控运行时替换，另提供显式单资源回收入口，不新增后台扫除。

**保留对象。** 在稳定 update 状态根下设私有 retain/，独立于槽位／current 指针；按受信资源标识记录 pin：实际包根（slot SHA 或 factory 的只读身份）、owner 实现／导入依赖闭包摘要、解释器调用路径与实际 exe 身份、sys.prefix／base_prefix、venv／标准库／site-packages 及 runtime lock 摘要、端点与 boot。不存环境值、凭据、CODEX_HOME 正文或 rollout；路径只可来自已核 launcher／owner 自身，不接受 registry 指定删除路径。venv 的正常解释器链接解析并固定实际目标身份，不扩大 PCE 根／锁／registry 禁软链规则。factory 与系统 Python／标准库作为不可回收只读基线；无法保证该基线保留的安装形态拒绝 private，不能声称只保留 python 可执行文件就足够。

**生命周期与竞态。** 新稳定 stdlib-only retention 核与 guard 分开于可替换包，提供非阻塞能力探测、注册、检查和回收门。owner 创建前，启动方核资源并持包／运行时共享 flock，先原子落 active pin、fsync，再将同一锁 FD 显式传给 owner；收到带原有同 FD 身份证明的 owner ready 后，启动方才关闭自己的副本。owner 在整个生命周期（包括 child 退避重启）持锁；child 不继承。helper 停止／升级不释放 owner 的锁或 pin。FD 交接失败／Popen 失败留 unknown；无 ready 不能假定没启动。retain 记录与 registry 均非进程控制授权，不凭 PID 文件领养、探活、杀进程或认定可删。

更新仍取 prepare.lock；涉及 slot 替换／保留集合切换时顺序为短 retain.txn.lock → state.lock。owner 启动在现有 endpoint launch.lock 内短取 retain.txn.lock，完成 pin 与共享 FD 后释放；回收持 retain.txn.lock，只尝试资源独占锁、不等待活 owner。持 state.lock 不回调 owner、不反取 endpoint 锁，网络下载不延长新 retain 事务；正常停止在既有准入／空闲门之后写证据。所有路径 FD／inode／权限前后复核，资源替换必须经同一事务，消除“查完 pin 后 owner 恰好启动”的窗口。

| 场景 | 决策与回收条件 |
| --- | --- |
| helper 升级／回滚，活 owner | 包＋解释器＋依赖仍固定原位；pin 加入 current／previous／pending 之外的保留集合。碰撞替换命中 pin 即拒绝，不能删后重建同 SHA。 |
| 新 helper 与 owner 兼容 | 不放宽一期 implementation／protocol／config／exe 强比；候选还须通过明确的 owner 依赖闭包兼容摘要。保持活 owner、原 ID；不把 registry 升格为授权。 |
| 实现／解释器／闭包不兼容 | arm 前拒 manual_required，原 helper／owner 保持；不自动停 owner、不借升级建新 owner。首期可保守拒绝元数据变化，不承诺任意升版均保 owner；后续按已批空闲维护流程处理。 |
| 空闲显式停止成功 | 原门核人工退出、无活动 turn，owner 真实 wait child 完成并精确清理端点；稳定核仅在已核同 FD 的 stop 事务收到匹配 boot／pin／资源身份／随机 nonce 的成功响应并核锁释放后，提交 stopped 回执。缺失响应则保留，不以 registry／任意 JSON 声称已停。 |
| owner 失联、child 状态未知、启动／停止中崩溃 | 即使共享锁消失仍保留 active／unknown pin；不自动回收，不推断 child 已死。待二期恢复单／另令处理，禁止按陈旧 PID 杀孤儿。 |
| 显式单资源回收 | 仅已核 stopped 回执对应原 pin、资源锁独占成功、非 current／previous／pending／factory、无任何其他 active／unknown pin 引用且身份未漂移时，才允许删自己 slot；共享 runtime 须所有引用均释放且非稳定基线。坏记录／超时／磁盘压力均保留并报阻塞。 |

**升级与回滚落地。** 私有模式启动前必须具备新版稳定 retention 核／guard 能力，旧 launcher 不支持则拒绝 private；稳定挂载件不能靠自升级源码包偷偷替换，另交受控迁移与回退单，先本组隔离验证。旧 journal format=1 不改，pin 独立存储；legacy 且无 pin 完全沿用旧行为。存在 active／unknown pin 时，候选预检和稳定 guard 的 trial／失败回滚／factory fallback 都须核兼容，禁止自动落到不识 pin／private 的旧 helper；无可用兼容版本则可见停止 helper，保留 owner 和全部资源，不能静默 legacy。源升级继续拒 lock_changed，不原位 pip／重建共享 venv；解释器维护只可在所有相关 owner 已按门停止后另批，第三方管理员绕过受控流程的修改不在自动保护承诺内。存储超限只阻塞新启动／升级并告警，不清未决 pin。设计通过不等于部署能力已具备。

**验收／撤修。** 隔离旧／新布局与官方 0.161.0／0.163.0：①连续两次升级使原 slot 已非 current／previous，owner 重启 child、延迟导入仍用原包／解释器，同 ID；②同 SHA 碰撞不能删 pinned slot；③启动与删除并发、FD 移交前后崩溃均无删除窗口；④owner 异常退出而 child 存活、旧 boot／错 PID／伪 registry／软链／inode 漂移／坏 pin／磁盘满均保留或拒绝；⑤正常空闲 stop 后只收单一无引用 slot，共享 venv 其他 owner 存活则不收；⑥不兼容实现／运行时、旧 guard、候选失败回滚与旧 factory fallback 均拒，不改当前绑定；⑦legacy 无 pin 升级／回滚原合同全过。每门指定撤修红；全套 helper（隔离 HOME）、canonical、全仓 Go／vet、升级／回退夹具，保留首轮红与来源清单。实现候选仍由组长审、prodops 交叉核；启用 private 与任何稳定 launcher 迁移均需另令。
