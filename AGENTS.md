# DingWei 仓 Agent 指令

本文件给在本仓工作的所有 Agent（Codex 读 AGENTS.md，Claude 经 CLAUDE.md 引入本文件）。只写不变的规则，不写当前在办事项或谁在做什么。

## 一、规格、读取顺序与写回

本仓规格用 OpenSpec（钉版 1.16.0，Node >= 22.12，`OPENSPEC_TELEMETRY=0`）。读取顺序、写回、单执行者、派单后规格变更同步、密钥与大文件等规则，全部以 `openspec/config.yaml` 的 `context` 与 `rules` 为准，本文件不重述。

## 二、源码交换与推送（config 未覆盖部分）

- 源码只经 Git 托管交换（写入点以平台公告为准），只交换仓库、分支、commit／tree 坐标；不用文件、消息正文或附件传源码、补丁或 bundle。
- 写前 fetch，禁止 force push；推送后核 `git ls-remote` 与本地一致再报坐标。
- 已交审的提交只追加、不改写。

## 三、构建与测试门

- 交件前必须跑 `scripts/run-build-tests.sh`，rc=0 才交；复核别人的交件同样要跑。只跑 Go 包不算。
- 发版构建只走 `SOURCE_COMMIT=<40 位 SHA> ./build.sh`，手写 `go build` 的产物不作发版件。
- Go 构建与测试以普通用户运行，不以 root 运行。
- 本机起真实 sessionhelper 或 Hub 做测试时，剥掉继承的 `SH_*`／`DINGWEI_*` 变量，并显式给数据库路径、监听地址与 HOME，避免碰到在线实例。
- 新增浏览器回归脚本要登记进 `tools/run-browser-regressions.py` 的 `SUITES`。

## 四、复核

- 不自验收：结论由别席复核，交件附可复核证据（分支、commit、命令、UTC 时刻、原始日志）。
- 每条判据附变异读数：删掉被测的保护必须转红，且红在该判据上。
- 跳过的用例写明用例名与跳过条件。
