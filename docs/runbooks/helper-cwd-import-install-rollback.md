# CWD 隔离钩子：逐席安装与回滚合同（待组长审、另令执行）

只在本组逐席做；当前文档不授权安装、重启或修改在线文件。组长 2026-10-22T23:05:43Z 后继裁定：随下次大版本并入集成线，不单独加急部署，覆盖此前“另一轮逐席安装”的排期。容器 Entrypoint/Cmd、工作目录、凭据及运行时依赖锁不变。r37 四席既定升级先行，不等本件。候选基 r37 29e63f99；不能只发 factory 自升级包就声称修完。

## 前置和制品

- prodops 先保存交接；只读核目标真实 venv 的 `sys.executable`、`sys.prefix`、`sysconfig.get_path('purelib')`、Python 版本、容器入口、cwd、PYTHONPATH 路径结构、源码挂载、所有既有 .pth/sitecustomize、venv 是否被其他服务共用。检查使用实际解释器加 `-I -B -c`；不要在可写项目下裸跑 Python。只接受受信根／venv、已核非组写／他写祖先、无路径软链替换、无不可信启动钩子和用户 site；本件不抵御能任意改写可信 venv 的主体。未知启动形态、共享范围不清或源挂载无法更新均 STOP。
- 生成器 `tools/sessionhelper/update_launcher/cwd_import.py` 只输出一个 .pth 文件，不触碰任何 venv。参数绑定**目标实际**解释器与稳定根；不得把 current 槽位写入参数。用从 Git 独取的候选生成，记 Git commit/tree、生成器 blob/SHA、输出 SHA 和字节数；不放入 factory、自升级压缩包、依赖锁或镜像。
- 稳定根中 `helper_update_guard.py` 和 `sessionhelper/mcp_shim.py` 必须是本候选的两份已审隔离子跳（分别来自 update_launcher/guard.py、mcp_forwarder.py）。若仍是 r37 旧文件，先在本席维护窗按既有 bootstrap **完整稳定根安装／备份事务**更新由本候选 Git 重建且验包的制品并核 MANIFEST，禁止仅 Console factory 更新；这一步另列执行单审批，不在 hook 中偷偷改根。不得覆盖已部署的 RETAIN 新 guard 为本分支旧基线：若已含 RETAIN，先合成并审查两件的并集制品。启动前确认 guard、MCP 与 hook 三面齐备。若该席显式配置了旧布局稳定入口，还须核已安装的 legacy_mcp_entry.py 为本候选隔离子跳；其独立入口只按既有迁移执行单更新，不改指具体槽位，不给组外席位新增迁移。
- 有活／未知 private owner、retain pin 时，修改其运行包／解释器闭包须先依保留件合同判 manual_required；不得为装钩子删除 pin、强停 orphan 或绕过保护。现有 legacy 无 pin 无新增保留目录要求。

## 钩子单文件安装（由 prodops 在批准窗口执行）

以下命令在目标席容器命名空间内的管理员环境执行，不能把容器内 SITE_DIR 误当宿主同名路径。取码须由执行方从 Git 独取并核精确提交；不通过消息／文件服务传源码。以下大写路径均由该席只读回执冻结，不能临时猜测。`PRIVATE_STAGE` 是 root 私有 0700 目录；`SITE_DIR` 是该 venv 已核 purelib。运行生成器的解释器可以是干净运维 Python，生成文件里的 `SEAT_PYTHON` 必须是该席真实 venv 路径。

```sh
python3 -I -B "$GIT_CHECKOUT/tools/sessionhelper/update_launcher/cwd_import.py" \
  --stable-root "$STABLE_ROOT" --interpreter "$SEAT_PYTHON" \
  > "$PRIVATE_STAGE/00-dingwei-helper-cwd-import.pth"
sha256sum "$PRIVATE_STAGE/00-dingwei-helper-cwd-import.pth"
```

先在隔离同构旧布局／实际 venv 的私有克隆中验证，再安排本席空闲并按既有监督器合同停 helper。不能以 docker exec 再起第二常驻 helper。检查 SITE_DIR 及目标无软链；目标不存在才是新增路径，存在时仅准已登记同制品幂等安装，未知内容 STOP。原 hook 如有先按字节、属主模式备份至 PRIVATE_STAGE，不覆盖备份。停机后，由管理员执行：

```sh
install -o root -g root -m 0644 "$PRIVATE_STAGE/00-dingwei-helper-cwd-import.pth" \
  "$SITE_DIR/.00-dingwei-helper-cwd-import.pth.new"
mv -T -- "$SITE_DIR/.00-dingwei-helper-cwd-import.pth.new" \
  "$SITE_DIR/00-dingwei-helper-cwd-import.pth"
stat -c '%U:%G %a %F' "$SITE_DIR/00-dingwei-helper-cwd-import.pth"
sha256sum "$SITE_DIR/00-dingwei-helper-cwd-import.pth"
```

临时路径亦须预先确认不存在、非软链；同一文件系统原子替换。前后校验输出与冻结 SHA 相同、root:root 0644、普通文件且非软链。不改变 venv 目录模式或任何依赖版本。然后依既有监督器启动本席；核唯一 helper、版本／conn_seq、IPC、dw_inbox、业务 cwd 不变；以该次启动日志 `HELPER_CWD_IMPORT_READY isolated=1 pid=…` 绑定首跳实际 PID（exec 保留 PID），结合该 guard 子进程 /proc/cmdline 的仅布尔 `-I` 核验、已冻结根／实际槽位 SHA 确认后续跳；不打印完整命令或环境，不以另起无关 Python 的 flags 代替。缺证、日志出现 `HELPER_CWD_IMPORT_ERROR startup_refused` 或双在线即 STOP／回滚，不重试抢连接。成功标记仅去敏 stderr，不进入 MCP stdout；缺此标记不能验收。

## 回滚

先停本席 helper，校验现 hook SHA 仍为本次安装值；不符 STOP，不删未知文件。首次新增则**只删除这一已核绝对路径的 .pth**；替换已有登记 hook 则原子恢复其备份并还原属主模式。稳定根若同窗更新，按其 bootstrap receipt 完整恢复原根，不能只回 guard 不回 MANIFEST。再启动本席并核唯一在线、旧版本／IPC及业务 cwd；明确记为恢复旧 cwd 风险，不称仍有隔离保护。无需重建容器、不卸载依赖、不清业务目录／状态库／凭据。

## 锁兼容与已知边界

`SelfUpdater.runtime_lock_sha256` 只散列稳定根 requirements-runtime.lock，validate_offer 另核其中发行包版本；普通 .pth 不是发行包，也不改锁字节。本候选不增依赖，原锁 SHA 维持。真实独立 venv 格覆盖装前／装后 validate_offer 均通过、真正改变锁仍 lock_changed；guard 选择／回滚沿用原账本。若未来校验扩大到整个 venv／launcher，必须重新评估，不能据此旁路。

钩子是单行绝对绑定制品；已 -I 或进程内已重执行标记 no-op，环境变量伪造标记无效。只识别 `-m sessionhelper[.子模块]`（前置支持 -B/-u/-E/-s/-P）；脚本、-c、其他模块保持原行为。-S 会禁用 site，未知前置参数不属已批准容器入口，禁止作为上线形态；必须在前置核出并 STOP。钩子损坏／缺失无法由其自身保证执行，靠文件 SHA、完整布局验收与实际新 helper 隔离诊断阻止验收。运行故障写固定去敏错误并 exit78，不把 .pth 的“报错继续”当降级方案。

集成提示：本分支 canonical 为60；RETAIN 分支为61。二者合并须保留测试清单并集，按实际清单同步 shell 回显与 deploy/test_build_entrypoints.py 合同（当前预计62），不能择一覆盖。
