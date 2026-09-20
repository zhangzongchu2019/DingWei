# `0.2.1.6` · C2 作业平台壳交付

## 实施状态

分支：`coder56solzzc/c2-console-shell-0216`

### ⓪ console 层浏览器测试装置

装置：`view-service/console-browser-regression.js`

装置不复制 console HTML。它从 `internal/m8/console_auth.go` 提取真实的 `consolePageHTML`，由本地 HTTP server 提供 `/console`；仅替换 `/api/console/agents` 与 `/view/*` 两个外部依赖，然后用 Playwright 驱动真实 DOM 与脚本。

当前三条用例分别验证：

1. 浏览器拿到真实 console 文档身份和 DOM；
2. `/api/console/agents` 的在线/离线对象真实渲染为按钮状态；
3. 点击在线 Agent 后真实创建 tab、激活态与 iframe 路径。

运行命令（本机 Playwright 安装路径按环境给 `NODE_PATH`；系统 Chrome 用 `PW_CHANNEL=chrome`）：

```text
NODE_PATH=<playwright-node_modules> PW_CHANNEL=chrome node view-service/console-browser-regression.js
```

### 可证伪证据

**先红后绿：**首轮把第一条用例指向不存在的 `#console-harness-probe-that-does-not-exist`：`rc=1`，只有 `loads the real console document` 转红，另外两条通过。改为真实 `nav #a` 后：`passed=3 failed=0`、`rc=0`。

**空白页变异：**只把装置的 `/console` 响应从真实 `consolePageHTML` 改为空白 HTML；`git diff --numstat` 为 `1 1 view-service/console-browser-regression.js`。首格时结果为 `rc=1`、`passed=0 failed=3`；平台壳及双角色浏览器用例接入后复跑为 `rc=1`、`passed=0 failed=5`，新增的平台项与 management 用例也转红。变异已还原。

### ① 角色权限模型

新增独立的 `console_platform_role`，角色为 `management` / `owner`。它不读取、复用或写入 `console_agent_grant`；后者仍只表达 principal × Agent 的 view/interact 权限。

服务端动作矩阵：

- 管理团队：read / submit / add / edit / delete / approve；
- 普通业主：read / submit；
- 未知或非活跃 principal：全部拒绝。

现有 active console principal 默认是普通业主；只有显式 active 的 management 记录才能取得管理权限。权限判定由 `RequireConsolePlatformAction` 在服务端执行，前端按钮可见性不参与授权。

定向回归：

```text
go test ./internal/store ./internal/m8 -run TestConsolePlatform -count=1
```

结果：两包 `rc=0`。测试明确不创建任何 Agent grant，仍能解析平台角色，证明两套语义没有混表。

变异：只把 owner 分支短路为允许全部动作，`git diff --numstat` 为 `1 1 internal/m8/console_platform.go`。定向命令执行 12 个子用例，只有 owner/add、edit、delete、approve 四个阴格转红；manager 六项与 owner/read、submit 仍通过。`rc=1`，还原后定向回归重新 `rc=0`。

### ② Agent 列表页拓展 ＋ ③ 功能项挂载框架

console 左栏新增 Work platform 区域，仍保留既有 Agent 列表与多 tab 行为。`GET /api/console/platform` 返回服务端已解析的角色、允许动作与挂载项；前端只据此渲染，不承担授权。

第一项真实挂载为既有 Agent workspace：API 返回 `id=agents`、`href=#agents`，浏览器用例验证该入口真实出现在 console DOM。它不是空扩展点。

后续 I/K/L 可复用的服务端框架已经落地：

- 普通业主 `POST /api/console/platform/submissions`：只生成 pending；
- 管理团队可 add / edit / delete / approve；
- 普通业主列表不显示 pending，管理团队可见并审批；
- 所有管理端路由均同时经过 console 登录与平台角色守卫；同一 HTTP 通道不会产生豁免。

角色可由 console seed 的 `platform_roles` 显式声明；生成器支持 `--management-principal`，其余 principal 显式落为 owner。未知 management principal fail-closed，种子与校验器的字段集合已有双向测试。

当前定向证据：

```text
python3 -m unittest -v deploy.test_generate_console_seed deploy.test_validate_console_seed
# 23 tests, rc=0
go test ./internal/store ./internal/m8 ./cmd/workpulse -run 'TestConsolePlatform|TestConsoleSeed|TestValidateConsoleSeed' -count=1
# 三包 rc=0
NODE_PATH=<playwright-node_modules> PW_CHANNEL=chrome node view-service/console-browser-regression.js
# passed=5 failed=0（普通业主与管理团队两侧均覆盖）
```

挂载变异：只删除服务端把内建 `agents` 项加入响应的那一行，`git diff --numstat` 为 `1 1 internal/m8/console_platform.go`。定向用例唯一转红为 `TestConsolePlatformHandlersMountAgentsAndEnforceWorkflow`，失败内容 `Items:[]`；`rc=1`。这证明“至少真挂一项”不是只由浏览器 fixture 自证。已还原。

权限阴格同时查响应与状态：普通业主直接调用 add 后既须得到 403，也须查询 `console_platform_item` 确认 `direct` 行不存在。只看其中一侧不算通过。

M-perm-x 单侧变异：拒绝分支先用丢弃响应的 recorder 真执行 handler，再对外原样返回 403。`git diff --numstat` 为 `3 0 internal/m8/console_platform.go`；因此响应断言保持通过，但数据库出现 approved 的 `direct` 行，`TestConsolePlatformHandlersMountAgentsAndEnforceWorkflow` 以 `owner received 403 but management side effect still happened` 精确转红，`rc=1`。已还原。

装配闸：`TestConsolePlatformRoutesAreAssembledWithServerGuards` 用 Go AST 读取 `cmd/workpulse/main.go`，要求六条平台路由同时存在目标 handler、console principal 守卫与平台角色守卫。它验证的是路由装配，不拿“handler 函数存在”冒充“功能已接上”。

装配变异：只删除 approve 路由，`git diff --numstat` 为 `0 1 cmd/workpulse/main.go`；定向测试 `rc=1`，唯一失败为 `console platform route not assembled: POST /api/console/platform/items/{itemID}/approve`。已还原。

全量回归（2026-09-20）：

```text
go build ./...                         # rc=0
go vet ./...                           # rc=0
go test ./... -count=1                 # rc=0
python3 -m unittest -q deploy.test_generate_console_seed deploy.test_validate_console_seed
# 23 tests, rc=0
console-browser-regression.js           # passed=5 failed=0
```

## 明确没有证明

- 尚未用真实浏览器登录生产 console；Playwright 使用真实页面源码，但 API 数据是本地 fixture。
- 尚未部署或触碰生产，未生成含真实身份值的 seed。
- 尚未把 I/K/L 的业务页面接入；本版只提供壳、角色与通用挂载/审批生命周期，真实挂载项是 Agents。

## `0.2.1.5` 合入后的最终装配

- 已合入发件人标识锚 `f6590f21bc1d6f0c172a90367871707e8016bcc8`；`git merge-base --is-ancestor` rc=0。
- 三处版本号已由 `0.2.1.5` 统一抬至 `0.2.1.6`。
- 相对该发件人标识锚的文件差异只含 C2 平台壳文件与三处版本号，没有夹带其它功能。
- 合并后基线：`go build ./...` rc=0；`go vet ./...` rc=0；`go test ./... -count=1` rc=0；console 浏览器 `5/5`；console seed Python 测试 `23/23`。
- 合并后 M-label：删 Agent 落款，全仓唯一红 `TestAgentFeishuOutboundBodyIdentifiesAuthenticatedSenderOnce`。
- 合并后 C2 变异均已复跑：owner 全放行会红四个角色矩阵阴格及状态型 handler 阴格；删 Agents 挂载只红 handler；删 approve 路由只红 AST 装配闸；保持 403 但执行副作用只红状态断言。所有变异均已还原。
