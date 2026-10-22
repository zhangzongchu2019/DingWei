# Codex daemon peer 身份门（候选，不部署）

设计：docs/designs/S-HELPER-DAEMON-IDENTITY.md；基r36，开发／真实listener实测仅coder56solzzc本席隔离环境，不触碰oaagent、dataConnector或在线helper。当前版本位不改，后续集成／升级由组长另定。

## 启用与兼容

- `SH_CODEX_DAEMON_IDENTITY_CHECK` 启动快照，三态为`observe`（默认）、`enforce`、`off`。observe仍每次核验，失败只记固定类别告警与identity_scope=unverified、照常连接；enforce才在身份不可证／不符时拒连；off不核验并记录identity_scope=unchecked。其他值（含旧候选1／0、空白／true）均拒连。改变后须在获批窗口重启目标helper；本候选没有修改任何现网开关。
- `SH_CODEX_DAEMON_EXE` 可选，指经核实际原生Codex可执行文件；未设则用既有SH_CODEX_BIN或PATH解析值。npm／shell wrapper不按名字猜二级目录，wrapper文件与实际/proc exe inode不同在observe中告警、enforce中拒连。升级前须只读核command目标、原生二进制SHA／可读权限、helper与daemon UID和/proc可见性，不符即停报，不临时关门冒充通过。
- 仅Linux daemon_ws可用SO_PEERCRED＋/proc核验；observe对平台／proc无法证明只告警；enforce不可证即拒，不静默降级。direct/proxy维持原合同，不能借此宣称其后端身份已核。出现明确身份拒绝只修已批准配置或回滚候选，不自动切direct、不猜另一个PID、不清旧thread账本。
- 门限于本次连接的peer UID与预期exe；当前daemon-start命令没有本组实测认可的可验证daemon PID合同，因此生产装配没有自启所有权证明。不能把命令PID、PPID1、同名进程、“最新PID”、首次连上的peer或不可信pidfile当自启证明。同UID同exe的两个daemon仍不可仅凭此门区分。
- `OwnedDaemonIdentity(pid,starttime)`仅是可信启动者API：调用方须实际持有所启动daemon句柄并记录其/proc starttime；本组前台Unix listener实测用Popen句柄验证该分支。生产driver_factory当前不传该绑定，明确标scope=uid_exe_only；未来启动方提供可信实例身份时另审接入，不新增任意PID环境变量。

## 可观测与停止条件

连接成功核验后记录daemon_identity_verified及peer PID／UID／starttime和检查范围。enforce身份不可证／不符时，在任何HTTP Upgrade和RPC之前关FD，RPC与初始化状态清空，结构化error事件为session_unavailable＋daemon_identity固定错误类别；无路径、凭据、原始异常或正文。投递返回并持久化failed/session_unavailable，重复同envelope只查账、不再尝试连接或投递，不新建／替换thread，不改变helper的投递ACK含义。重连从新FD再核，不沿用旧结果。socket路径被替换不会搬走既有连接；这不是“自动找正确daemon”的功能。

## 隔离验收与恢复

1. 全部本地夹具使用私有HOME／CODEX_HOME／短TMPDIR，env-i等价白名单环境与既有锁定venv，真实AF_UNIX／SO_PEERCRED，同时有精确UID／proc故障注入。不构造在线helper，不连生产Hub。
2. `tests/daemon-identity/real_protocol.py`只运行本地已有、与前序Git provenance完全匹配的官方0.161.0／0.163.0原生二进制；每版5格：Popen可信PID＋starttime及重连、UID＋exe弱绑定、错exe、错starttime、重启为另一PID后拒旧绑定。只initialize，不建thread／不发模型turn；新进程HOME与CODEX_HOME隔离，provider是本地拒绝端点，凭据环境不继承。finally只终止并回收本脚本Popen持有的进程，绝不扫描／停止现有daemon。
3. 定向 `test_daemon_identity.py`；撤修器 `tests/daemon-identity/check.py`在新私有detached worktree执行指定语义红、精确恢复绿；helper全套、canonical从冻结源在隔离HOME＋锁定venv运行。与待合MCP-SLOT-PIN都各加一个canonical入口：本分支58，r37同时合入两件时应调整到59并同步入口计数测试，不可机械沿用58。
4. 本件不提供线上GO。将来部署须先在DingWei本组核安装与exe事实、记录检查scope，再逐席窗口升级；组外helper保持原状。回退先回候选前已审helper并恢复原私有配置／重启范围；先在获批本组席位observe采集scope与告警，确认exe等无误报后逐席另批enforce；没有自动升级模式。旧代码不识此三态，回退须恢复原配置，不把off声称为身份验收。禁止为恢复可用性启动替代thread、改账本、杀未知daemon或扩大到组外。
