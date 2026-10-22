# S-HELPER-CWD-IMPORT 实现交件

基 r37 29e63f9963dedcc32010ca89265d78ba12664bf2，设计 3d46b6b7 已获组长 PASS。最终冻结输入 c8c023c289b254f9f8a3ad8ca4ac1357e21071d0，tree f12efa2d60ed7487105b1d5b4313019f84b72384，标签 evidence/coder/helper-cwd-import-f12efa2d；产品实现同父 4386c50c，后继仅构建合同测试计数及门脚本 verifier 选择修正。VERSION 仍 0.2.3.10；不部署，随下一大版本集成，不阻 r37 当前升级。

## 合同与实现

- `tools/sessionhelper/update_launcher/cwd_import.py` 仅生成一个绝对路径绑定的 .pth，不安装。venv site 阶段只匹配 helper 模块入口；已 -I 或进程内重执行标记 no-op，环境伪造标记无效。以原 venv exec -I，显式置顶可信稳定根后才导入 runpy；业务 cwd、参数、SH 环境及 CODEX_HOME 不变。错误固定去敏 stderr 并 exit78，避免 site.py 打印异常后继续；成功记录实际 isolated 位与同一 exec PID，不污染 MCP stdout。
- guard、新布局 MCP forwarder、旧布局稳定 legacy_mcp_entry 三处子跳都 -I，显式包根先于 runpy；不改变槽选择、账本、IPC、MCP 协议或权限。未改 factory/sessionhelper 运行码、Hub、Go、依赖锁或版本位。
- 仅 helper 模块命中；同 venv 普通脚本、-c、其他模块保持原行为。只支持已核 Python 与入口形态；-S／未知前置参数不是批准部署形态，执行前必须拒绝，不声称可保护任意 Python 调用。
- `SelfUpdater.validate_offer` 在真实隔离 venv 中安装前后均接受同锁，实际改锁仍 lock_changed；guard 不把 .pth 当依赖发行包。精确 r37 verifier 接受本地重建包，根集合沿用 r37，hook 不在 factory／升级压缩包／锁中。生成器及 .pth 须走独立受审部署。

## 作者证据

| 门 | 最终冻结输入读数 |
| --- | --- |
| 全套 helper | env-i、隔离 HOME、锁定 venv，867 项 OK，rc0 |
| canonical | BUILD_TESTS_OK python=60 shell=2，rc0 |
| 本件定向 | 18 项含实际新 venv 启动、参数／cwd／环境、幂等、可见失败、安装增删、runtime lock |
| 撤修 | 11 项全部指定 AssertionError 红、恢复绿；含 cwd 假包／json／sitecustomize，三种子跳隔离、fail-closed、日志、非 helper 与幂等、根模式 |
| 验包 | 精确 r37 29e63f99 verifier rc0，hook 不在包成员；本地包未上传、未登记 |

原始命令／日志／rc 在 final-successor，源码 blob／大小／SHA 在 SOURCE.json；运行本目录 VERIFY.py 核全部源码与证据。实际运行 Python 3.12.3；不将隔离夹具等同已验证所有生产 venv。例示 hook 参数是历史已知路径，未独立核现网，正式逐席生成须以该席读数绑定。例示 hook SHA a14dd4e8813c0dd51cc25d4fcdcb01939a6a6036d36a37a56c04e4f451cbd71a，2904 字节；锁 SHA 5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0。

## 原始失败与上线限域

首轮冻结 4386c50c 的 helper 867 OK、11 撤修通过；canonical 因本席漏同步计数（59→60）rc1；打包门因误用旧布局 b5 verifier 报 manifest_invalid 中止。日志和 OUTCOME.md 原样保留，未称首轮全门通过；正确新布局根包含 r37 已有 MCP shim。最终 c8c023c2 完整重跑通过，没有放宽产品安全门。

安装／回滚：docs/runbooks/helper-cwd-import-install-rollback.md。钩子单文件新增／删除、root:root 0644、SHA 前后核、容器命名空间和唯一 helper 门均列明；受信 venv／根若无法核实、共享范围不清即 STOP。稳定 guard／MCP 更新须另审完整根事务，不能只做 factory 更新；不能把已含 RETAIN 的根回退成此分支旧基线。活／未知 owner 仍按 pin 合同 manual_required，不能旁路。回滚明确恢复旧 cwd 风险。

集成时 RETAIN 与本件 canonical 清单须取并集，按实际清单同步回显与合同（当前预计62）。本件提请组长审，不自作独立验收；没有改容器入口、重建／重启容器或接触任何在线席位。
