# S-R53-TEMPGROUP-CONSOLE-UI 证据

## 基线、范围与依据

- 分支：`k31t2075/r53-tempgroup-ui`。
- 父提交：`144b215e32e718553d9fef2e79c83ece164f6514`。
- 提案：GW-010-S03～S06。
- 裁定：`review/opus50tc2k/rulings` 的 `4dd09d26391c50fe6dbba314cfb25cbd6ada1ce3`，㊹-补3、㊹-补12。
- 接口依据：`review/qwen38mvb4/tempgroup-survey-20261106` 的 `af14c02c66b7367be4455476343ac442dec155a2`，`REPORT.md` 第 4.7 节。
- 改动仅包括 `internal/m8/console_auth.go` 的页面部分、新浏览器回归及其统一 runner 登记、本证据文件。未实现或修改后端，未写仓根 `EVIDENCE.md`，未在页面源码增加版本号字样。

## 接口字段与页面行为

| 接口 | 页面使用字段 | 行为 |
|---|---|---|
| `GET /api/console/temp-groups` | `temp_groups[].code`、`external_full_name`（缺省退到 `external_short_name`）、`expires_at`、`dissolved_at`、`dissolve_reason` | Agents 列表后的独立块展示 AG 码、对端外部全名、剩余有效期和状态；空数组时侧栏及属性节均 hidden |
| `GET /api/console/temp-groups/{code}` | 上述单项及 `members`、`created_by_display`、`created_at`、`last_used_at`／`last_use.at`、`expires_at`、`can_dissolve` | 属性页表格选中 AG 后取详情；严格要求两名成员，显示创建者归属名和创建／最后使用／预计解散时刻 |
| `POST /api/console/temp-groups/{code}/dissolve` | 请求 `{expected_expires_at: group.expires_at, reason: "owner"}`；204 或错误正文／`message` | 沿用 `confirm(...)` 二次确认；取消不发请求；成功只刷新临时 AG 列表；409 展示服务端可读原因 |

`refreshAgents` 的既有 15 秒调用结束后触发 `refreshTempGroups`，未新增 `setInterval`。页面保存请求代际，过期列表／详情响应不覆盖新选择。动态文本全部使用 `textContent`，AG 码进入 URL 前使用 `encodeURIComponent`。

无外部席位不变量的浏览器判据是：首个临时 AG 请求暂不返回，记录 `nav.innerHTML`；随后返回 `{"temp_groups":[]}`，再次读取。两次字符串必须逐字节相同，且新块保持 hidden。测试不是只检查 CSS 可见性，也不要求删除本功能初始隐藏的占位节。

## 浏览器回归覆盖

文件：`view-service/console-temp-groups-regression.js`；登记名：`console_temp_groups`。页面由现有 `console-hosted-page-fixture` 从生产 Go 页面实时生成，Chromium 只访问本地 HTTP 假接口，不读取真实平台 Agent 清单。

| 判据 | 实测 |
|---|---|
| 无 AG 整块 hidden 且 DOM 不变 | 空响应前后 `nav.innerHTML` 完全相等；隐藏属性保留 |
| 有相关 AG 时显示 | 侧栏显示「与我相关的临时编组」、AG7、alice_AG7.owner、剩余有效期和「有效」 |
| 既有节拍 | 调用一次 `refreshAgents` 后临时 AG GET 恰增加一次；源码保留 `setInterval(refreshAgents,15000)`，无临时 AG 新定时器 |
| 属性表格和详情 | 一行 AG；恰好两名成员；创建者 Owner Alpha；三个 `time.datetime` 分别等于假接口 RFC3339 原值 |
| 取消确认 | `confirm=false`，解散 POST 数为 0 |
| 409 可读错误 | 返回中文 `message` 后页面显示「临时编组已被其他人解散，请刷新后重试」 |
| 解散成功刷新 | 请求体含原 `expected_expires_at` 与 `reason=owner`；204 后临时 AG GET 恰增加一次，空结果隐藏侧栏及属性节 |
| 浏览器运行错误 | `pageerror` 为 0 |

正常输出：

```text
TEMP_GROUP_UI_BROWSER_OK empty_dom=byte_equal sidebar=true detail_members=2 cancel_requests=0 conflict_readable=true success_refresh=1 page_errors=0
```

## 两刀变异

执行日期：2026-11-06，UTC。每刀只临时修改生产页面，运行同一个真实浏览器脚本，随后立即还原；还原后正常脚本重新通过。

| 变异 | 命令 | 有效读数 |
|---|---|---|
| 将侧栏 `section.hidden=groups.length===0` 改为恒 false | `NODE_PATH=/tmp/k31-playwright/node_modules PW_CHANNEL=chrome node view-service/console-temp-groups-regression.js` | rc=1；空响应后 `<section id="tempGroups">` 失去 hidden，直接失败于 `empty temp AG response must leave nav DOM byte-for-byte unchanged`；还原后 rc=0 |
| 将解散的 `if(!confirm(...))return` 改为只调用 confirm、不返回 | 同上 | rc=1；取消确认仍产生一个 POST，断言为 `cancelled confirmation must not send dissolve`，实测 `1 !== 0`；还原后 rc=0 |

hidden 变异首次运行红在等待 hidden 的 30 秒超时；为加强读数，测试改为空响应后先比较 DOM 再查可见性，上表记录最终直接 DOM 变异读数。

## 定向与回归命令

- `gofmt -l internal/m8/console_auth.go`：无输出；`git diff --check`：rc=0。
- `go test -count=1 ./internal/m8 -run 'Console|Group'`：rc=0，45.395 秒。
- `go vet -composites=false ./internal/m8`：rc=0。
- `node view-service/console-group-live-regression.js`：rc=0，既有新鲜在线覆盖及单一定时器判据通过。
- `node view-service/console-groupsnapshot-refresh-regression.js`：rc=0，既有 2 项通过。
- 新 Chromium 脚本：rc=0，输出如上。

### 基线既有诊断，不计本单产品红

- `go vet ./internal/m8` 另跑时 rc=1，检出 `internal/m8/feishu_sticky_modes_test.go`、`feishu_sticky_test.go` 对 `store.FeishuStickyKey` 的既有非具名字段字面量。上述文件在本单无 diff；关闭该既有 composites 检查后其余 m8 vet 通过。
- `python3 tools/run-browser-regressions.py --selftest-env-coverage` 在基线既有 `renderer-epoch-order-regression.js`／`renderer-timeline-order-regression.js` 的 `TIMELINE_DEBUG` 未登记处 rc=2。本单 runner diff 只增加 `console_temp_groups` 登记，新脚本只使用已登记的 `PW_CHANNEL`，未越界修旧登记项。

## 完整门

- 开始：2026-11-06T08:00:56Z；完成：2026-11-06T08:10:50Z。
- 命令：`PATH=/tmp/k31-feishu-build-venv/bin:$PATH TMPDIR=/tmp/k31bt.rISiTh ./scripts/run-build-tests.sh`。
- 结果：rc=0；`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，helper 双配置各 1097 条预期用例。
- 原始日志保存在执行环境 `/tmp/k31-temp-ui-build-tests.log`，未把大体量日志提交到 Git。

## 边界与未冒充的验收

本单只验证前端和假接口接线。服务端相关 AG 过滤、无关人拒绝、代际与 `expected_expires_at` 权威现核、审计、通知、在途消息结算及普通组隔离均由 D 路／集成路实现并另行验证。本件不以假数据宣称这些后端判据已通过，也没有跳过本单新增浏览器用例。
