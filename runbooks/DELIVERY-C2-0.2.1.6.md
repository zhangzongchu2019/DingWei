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

**空白页变异：**只把装置的 `/console` 响应从真实 `consolePageHTML` 改为空白 HTML；`git diff --numstat` 为 `1 1 view-service/console-browser-regression.js`。结果 `rc=1`、`passed=0 failed=3`，三条分别红在标题身份、Agent 按钮渲染、tab/iframe 交互；已还原。

## 明确没有证明

- 本格只证明 console 浏览器装置有分辨力，尚未实现或验证 C2 的角色权限模型、Agent 列表拓展、功能项挂载框架。
- 本格没有启动真实 workpulse、没有登录真实 console、没有触碰生产。
- 本格使用 mock API 数据，只证明浏览器层消费真实页面代码；服务端角色权限须由后续独立的服务端阴格验证。

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
