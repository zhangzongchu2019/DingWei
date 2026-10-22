# 旧布局 MCP 稳定入口（候选；没有在线执行授权）

本件入口为 `tools/sessionhelper/legacy_mcp_entry.py`，不是 helper 自升级包成员；只经 Git 取已审 commit，再单独安装。基 r35，旧 `helper_update_package.py`／guard／factory／runtime lock 均不替换。设计见 `docs/designs/S-OAAGENT-MCP-SLOT-PIN.md`。本组目前没有已确认的真实旧布局席，验收限 coder 本席隔离夹具；oaagent 的一次性迁移须留到其下次升级另批，本件不得触碰该席或 dataConnector。

## 一次性迁移合同（执行单待另批）

1. 只读核真实安装根的 factory、实际 venv Python、SH_STATE_DIR 对应绝对私有目录、journal/current 已确认SHA与版本、MCP段command/args、既有IPC键与其通道匹配。源路径允许venv Python本身为常见软链，但交给入口的 state/factory 必须使用核定的规范绝对路径，不接受路径中的软链别名。没有pending升级；事实不符即停，不新建journal或修权限。原配置、current/factory与venv锁SHA留私有回执，不贴凭据值。
2. 备份完整Codex配置到本席0700目录、备份文件0600并记摘要；配置可能含秘密，不上传、不入Git。私有state目录已有且同UID／0700，独立建 `mcp-entry` 0700，以临时文件＋原子rename安装本件经审入口，文件0600。同目录安装动作在授权窗口，不由入口自身执行。
3. 仅改既有DingWei MCP段：command是已核实际venv Python；args是稳定入口绝对路径以及 `--state-dir`、`--factory-root` 两个明确值。不改env、IPC路径、其他MCP段或超时。用结构化TOML解析验证，只允许该段command/args差异；写配置须原子替换、保原属主模式。不要把此文的占位路径直接应用。
4. 获批后重连对应MCP；若客户端不能独立重连MCP则重开对应Codex会话。核initialize版本与journal.current一致、tools/list符合该版本、一次只读dw_inbox status及获批本组send_message往返，helper在线连接／IPC socket不变；不启动另一个helper、不以同凭据连Hub做握手探测。factory回落stderr有固定 `factory_fallback`，initialize仍报factory真实版本，不能记为升级成功。
5. 失败按原SHA恢复完整原配置／command/args，重连同一MCP。入口保留备查，不删槽、不动journal、不停helper、不做新旧布局迁移。以后Console升级确认后只需重连MCP并重复第4步，不再逐个手改槽位路径；现存MCP不热换模块。

## 选择与兼容限域

入口只读format=1的current快照；旧 b5faf73c guard真实arm/select/confirm/fail转换已纳入隔离测试。pending／previous永不作为MCP候选。state与update必须同UID且无组／他权限；slots、slot、包目录同UID且无组／他写。journal同UID、无组／他权限、常规文件且≤1MiB，以NOFOLLOW＋NONBLOCK读；关键包文件为同UID常规文件，不接受软链/FIFO。factory路径必须是核定root／本席UID所属的完整包，目录及关键文件不可组／他写。状态不合格回落factory；factory无效则固定错误和exit78、stdout空。选定shim执行后失败直接透传，不尝试另一份shim，不重放请求。只保证本地常规文件选择不等待FIFO/锁，不承诺故障文件系统实时性；不抵抗能改同UID私有目录的本机攻击者，安装前需核可信factory与槽位来源。

已确认槽和factory都能运行真实MCP stdio；旧factory可能只提供旧工具，入口不伪装版本、补工具或放宽IPC权限。模块入口以新解释器和显式sys.path优先级隔离cwd/PYTHONPATH污染，继承原业务环境（凭据不打印）。入口不导入guard/SessionHelper、不写状态、socket或升级预算，不改变旧verifier。自动跟随只在新MCP进程启动时发生；一次确认与选择并发可选旧current完整快照，重连后再核版本。

## 本地验收方法

在完整Git历史检出中使用锁定runtime venv；测试从不可变 `b5faf73c8fe4d5c71720daf4eed5b133b8d45dbd` 直接读取旧verifier、guard和旧shim原文字节（无需联网；浅克隆缺对象即失败，须先经Git补齐，不跳过）。夹具包是本地合成旧格式载荷，不是可发布升级包；真实旧verifier验包并拒绝加根mcp_shim的错误布局，真实旧guard驱动pending→confirmed→失败回退→第二次确认。真正旧helper进程和真实席位升级均未执行。

定向：`python -B -m unittest -v tools/sessionhelper/tests/test_legacy_mcp_entry.py`。撤修：以同一锁定解释器运行 `tests/mcp-slot-pin/check.py`，参数为不存在的私有输出目录；它建立隔离detached worktree并保留每格原始stdout/stderr/rc，指定断言红后恢复绿。全套helper须在 `env -i`、临时HOME／短TMPDIR、最小PATH、源码 `PYTHONPATH=<检出>/tools/sessionhelper:<检出>/tools/sessionhelper/tests` 和锁定venv下 `unittest discover -s tools/sessionhelper/tests`；canonical同样隔离运行 `scripts/run-build-tests.sh`（58 Python＋2 shell）。不要在真实HOME跑helper测试；正式门与撤修输出随交件入Git，线上验收另批。
