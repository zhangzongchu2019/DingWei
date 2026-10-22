# S-OAAGENT-MCP-SLOT-PIN 实现交件（仅本地隔离，待组长审）

设计 `7ab98883` 已由组长 PASS 后开实现。基 r35 `9e73ff1ff5769c2bf6f0098f5a843d90c232a39c`，冻结源 `3e3019e35d8f0e4dda860eb10be7d1e740a41b79`，tree `3a6057e047065c252c8f6839a7cf7d24fb7d0fa7`；后继只补 docs/证据。分支 `review/coder/mcp-slot-pin`。入口、设计、运行说明分别为 `tools/sessionhelper/legacy_mcp_entry.py`、`docs/designs/S-OAAGENT-MCP-SLOT-PIN.md`、`docs/runbooks/legacy-mcp-slot-entry.md`。

## 最终行为

独立入口安装到本席私有state内固定位置，MCP配置只需一次改command/args；运行时不安装、不修配置。每次进程启动只读format=1的已确认current，验证SHA、私有目录属主／模式、规范路径、journal常规文件／非跟随／非阻塞／1MiB上限、包完整性；pending／previous均不选。状态不合格回落核定factory，stderr固定提示且initialize保留真实旧版本；factory无效则stdout为空、固定错误、exit78。exec后shim失败直接返回，不切另一shim、不重投。继承原IPC环境，以同一个venv新解释器和显式导入顺序排除cwd/PYTHONPATH污染；入口不导入guard/SessionHelper，不写状态、预算、锁或socket，不连Hub。

已运行MCP不热换代码。确认后重新连接MCP（不支持单独重连时重开该Codex会话），新的MCP才跟随current；以后不再按槽位改配置。私有目录被同UID攻击者修改、故障文件系统硬实时、长期MCP缓存不在本件保证内。factory回落不代表升级成功，未执行自动权限修复、布局迁移或工具补授。

## 独立本地可重现门

最终冻结源：隔离HOME＋锁定venv helper全套824项（新增14项），rc0、无跳过；canonical BUILD_TESTS_OK python=58 shell=2，rc0；16项撤修均命中指定AssertionError红，恢复绿，含首尾整套基线共34子命令。最终helper耗时414.916秒。CHECKS.json和raw/保留全部原件；本件未运行任何在线验收。

新回归 `test_legacy_mcp_entry.py` 共14项，含逐条件subtests：current-only、两次确认切槽、坏journal/路径/权限/关键文件、FIFO等特殊文件限时回落、无有效factory固定失败、真实新shim的initialize/tools-list、旧b5 shim自报0.2.3.2且旧工具集、伪造环境下的真实dw_inbox→单次本地假IPC、已运行shim不热切、选中shim退出不回落、exec异常去敏、原子journal替换。

旧兼容直接从完整Git不可变 `b5faf73c8fe4d5c71720daf4eed5b133b8d45dbd` 读取真实稳定verifier／guard／shim／init并运行，字节与blob摘要见 INPUT-PROOF.json。verifier验的是合成旧格式包（惰性载荷与夹具锁，不是发布制品），真实旧白名单仍拒绝新增根mcp_shim。真实旧guard完成arm→trial→confirm A→trial B失败留A→再确认B，入口分别只见factory/A/B。没有启动旧helper进程，没有执行Console或真实席位升级，不能冒称实席端到端验收。

## 原始失败与限域

首轮helper全套漏设源码PYTHONPATH，469项、rc1、27个import错误（26个缺sessionhelper；1个测试包交叉导入失败）。同一冻结源只补正运行环境后重跑；首轮stdout/stderr/rc和启动元数据完整保留，未删除测试、改断言、守卫或产品来洗绿。正式命令使用env-i等价的显式环境、私有临时HOME／短TMPDIR、锁定venv和源码PYTHONPATH；venv来自本席既有 `/tmp/coder-0232-locked-venv`，未新增依赖或修改runtime lock，锁SHA仍5ef8d581。原有ResourceWarning／假WS异常输出保留，不等同单测失败。

canonical是原有正式入口，增加本件登记后为58 Python＋2 shell；没有添加pip安装步骤。其既有Go跨运行时测试在隔离HOME中下载了go.mod已锁定的依赖，未宣称整个canonical禁止联网或零下载；MCP选择器与撤修器本身无第三方依赖／联网安装。Go代码、embed资源、Hub／页面、helper主包、升级器、VERSION和runtime lock与r35均逐路径Git零差异；全仓Go／vet沿用已验收r35，不把本件canonical中的定向Go误称全仓Go。输入证明明确列出本件五个非docs变更。

## 迁移与回滚边界

只在coder56solzzc本席隔离夹具测试；本组目前无已核实的真实旧布局席。本候选不改任何在线MCP配置／helper／会话，不升级dataConnector或oaagent。oaagent一次性迁移留到其下次升级、届时另批：备份私有配置，装经审入口，仅改command/args，重连MCP并核版本／工具／只读状态／授权往返；失败恢复原配置并重连，不改journal、不删槽、不停helper。运行说明给执行前门，当前没有可直接对在线席执行的GO。

证据由 SOURCE-MANIFEST 锁定全部非docs源、DOC-MANIFEST锁定设计和运行说明、EVIDENCE-MANIFEST锁定本目录原件；VERIFY从Git对象重验，不依赖工作区文件。最终审查仍归组长与prodops，不构成部署授权。
