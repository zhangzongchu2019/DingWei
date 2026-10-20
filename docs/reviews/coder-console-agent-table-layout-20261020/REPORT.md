# S-CONSOLE-AGENT-TABLE-LAYOUT 作者交件

基 r33 `ab0f214496e6a5df1c71826cc978f40528923b8f`，分支
`review/coder/console-agent-table-layout`。冻结源与测试输入为 Git tree
`31dfb4853e3df0d58f656544d371170ef8bc9354`，标签 `evidence/coder/console-agent-layout-31dfb485`。
组长审查、prodops 交叉核待进行；随下一批，不部署。

## 修改与原因

原共用 group-table 使用 fixed 布局，并把前四列分别设为 28%、28%、28%、16%，
已经占满 100%，五列以上的 Agent／审核／账号等表因此挤压后面的列。

- 取消四个百分比与 fixed 布局，改为 auto 内容列宽、表格按内容占宽，
  短列不抢宽；表头不换行。Agent／主机／组名等长名称限制为
  clamp(12rem,25vw,24rem)，省略显示并保留完整 title；托管席追加的模型／
  配额提醒亦保留在完整提示中，主机完整内容可悬停查看。
- 最后一列保留最小 7em，操作单元格及按钮不换行；托管席四按钮同一行。
- 八张静态共用表格均套独立横向滚动容器（审核、Agent、编组、LLM 账号、
  平台用户、配额、托管主机、全部 Agent）；动态 helper 表同法。
  容器可键盘聚焦并有区域说明。窗口内主页面不横向溢出。
- 复选框解除文本输入的 320px 最小宽限制，长状态文本允许换行；
  编组等下拉框不超过容器宽度，避免超长 Agent 选项撑开整个工作区。
- helper 升级入口放在 Agent 列表标题工具栏右侧。它是整表批量升级入口，
  放工具栏比重复到每行更符合现有批选语义；原升级面板、批选、单行升级、
  延迟加载、取消过期请求及关闭/重开生命周期均保留。

仅两个生产页面资源和一个新增浏览器脚本有非 docs 差集。
`INPUTS.json` 校验 Go 文件在 consolePageHTML 常量之外的字节与 r33 完全相同，
helper 客户端及依赖锁零差集；没有改接口、授权、Hub 状态机、迁移或版本号。

## 实测

- 三个 viewport **1440／1024／768**：真实 Console HTML、真实 helper UI JS，
  Chromium + 本地 HTTP 夹具；三张表（Agent／审核／编组）的 header／操作列样式
  与按钮文字 Range 实际矩形断言，托管四按钮数量与同一行断言。
  768 实际 scrollLeft 大于 0，长名称确实裁切并保留 title；
  宽屏短表不满宽拉伸；页面无额外横向溢出。
- 点击审核打开编辑器、吊销请求、托管重启请求、工具栏展开及单 Agent helper
  升级请求；验证不合资格旧 helper 按钮仍 disabled，catalog 仍只在展开时读取。
  原有升级脚本的批选与关闭/重开等既有套件一并通过。
  展开并发送后的状态文本、Console 工作区与根页面均验证无横向溢出，
  复选框实际宽度小于 32px。
- 最终截图见 `screenshots/`：各宽度审核、Agent 左端与操作端、helper 面板；
  JSON 几何读数在 `screenshots/RESULTS.json`。
- **31 项既有 Console 相关回归脚本 + 1 项新增布局脚本全部 rc0**，
  详见 `browsers/FORMAL-BROWSERS.json` 和原始日志。历史真 Hub 的
  console-spec-task-p1-real-regression.js 未纳入此夹具矩阵；
  task-board 原有 K13 deferred 口径未改。正式矩阵完全在最终页面资源冻结后运行；
  旧的微调前日志只作为前序记录，不计最终门。
- 9 撤修逐个 rc1 且命中指定断言，取消撤修后 rc0。
  `fixed-columns`→LAYOUT_CONTENT_COLUMNS；actions-wrap→LAYOUT_ACTIONS_NOWRAP；
  buttons-wrap→LAYOUT_BUTTON_SINGLE_LINE；no-scroll→LAYOUT_SCROLL_CONTAINER；
  no-ellipsis→LAYOUT_NAME_ELLIPSIS；helper-below→LAYOUT_HELPER_TOOLBAR；
  wide-checkbox→LAYOUT_COMPACT_CHECKBOX；status-overflow→LAYOUT_STATUS_TEXT_WRAP；
  wide-select→LAYOUT_EXPANDED_PANEL_NO_PAGE_OVERFLOW。
  全部只改浏览器内的临时 HTML/JS，不改工作树。
- Go 1.22.2，隔离 Git tree 导出、隔离 HOME、离线依赖、无外部 DSN：
  vet rc0；canonical `BUILD_TESTS_OK python=55 shell=2`；
  全仓默认并行 `go test ./... -json -count=1 -timeout=30m` rc0，
  共 65 包，动作 {'skip': 13, 'pass': 52}，
  条件跳过 30 项逐项列 SUMMARY.json。
  全仓 UTC 2026-10-20T23:29:52.095658+00:00 至 2026-10-20T23:41:14.746931+00:00。

## 前序读数与闭合

前序输入 b88c148e 的浏览器／6 撤修／Go 门保留作预备读数，不替代最终门。
视觉复核增加复选框和状态文本检查时，status-overflow 撤修最初 rc0 存活：
当时只看 document 根的宽度，会漏掉 overflow:auto 工作区内的溢出。
补入状态元素和工作区实际宽度后，该撤修指定红，但恢复检查又发现
长 Agent 选项撑开编组下拉框。给 select 加 max-width:100% 后
最终 9 撤修指定红、三宽度恢复通过。中间原始存活日志与恢复失败日志
保留在 preliminary/，不计入 PASS。代码与测试没有通过放宽断言掩盖问题。
最终 Go 门等前序全仓结束后再串行启动，避免两轮 embedded PG 并发。

## 限域

截图与按钮请求证明本地浏览器布局／交互，不能代替线上认证验收或实际升级结果。
本件验证未向生产 Hub 发送测试请求，未连接 43、oaagent 或试点席位；
日常汇报沿用已在线 helper。未构建发布包、部署、重启或升级。
完整 helper 客户端测试本件未重跑，客户端源码未变；canonical 内的 helper 子集为本次新读数。
复跑命令和冻结输入取件见 REPRODUCE.md；VERIFY.py 从 Git blob 校验源码和证据清单。
