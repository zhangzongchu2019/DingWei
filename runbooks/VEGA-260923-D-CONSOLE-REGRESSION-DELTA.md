# Vega-260923-D：共享 Console 浏览器回归断言变更清单

范围：`view-service/console-browser-regression.js` 相对基线
`0eb7b91ea805ab4bfdf4beccca4ca7d5b5812788` 的每一项被移除或改写的断言。

| 基线断言 | 本版处理 | 理由 | 未降低的保证 |
|---|---|---|---|
| `#g .group-code` 为 0 | 改为 `#groupJoinCode`、`#groupCodeValue` 均展示完整 32 位组码 | Vega 规定组码仍是加入凭证；隐藏组码会使跨业主加入不可达。 | 同一用例读取两处实际值并验证复制到剪贴板。 |
| `.copy-group-code` 存在且复制后写 `#group-status` | 改为稳定 id `#copyGroupCode`，复制成功写 `#groupManageFeedback` | 新版把反馈贴近实际控件，避免全局状态被其它动作覆盖。 | 仍实测真实 Clipboard 内容和可见成功反馈。 |
| 下拉显示完整 session 名 / `data-key-id` | 改为短名、且不暴露 key id | 本版 UI 指标要求候选显示短名、不显示 ID。 | 仍断言候选仅为本业主、在线、且不在组内的 Agent；请求体仍仅含 `agent_id`。 |
| 所有 group workspace input 数量为 2 | 删除数量断言，改为按 id 断言组名与 32 位组码两个语义控件 | 输入总数不是产品契约；会把新增无关辅助控件误判成回归。 | 组名、组码、隐藏 key/id 字段分别以稳定 DOM id/选择器断言。 |
| 8 位 `[a-z0-9-]` 的前端拒绝 | 删除，改为空名可见拒绝及 1–8 中文名真实键盘输入 | 业主已将命名规则改为 1–8 字符、中文/字母/数字并清除无效 `pattern`。 | 后端名称合法性、trim、精确重复与 8 字符建议由 Go/vega 浏览器回归覆盖。 |
| 创建成功后全局 `#group-status` 为空 | 改为名称控件旁 `#groupNameFeedback=操作成功` | 本版核心是成功/拒绝必须可见，清空全局状态正是被修复的静默成功。 | 请求载荷、响应后刷新与可见反馈均断言。 |
| 已在组内的 `agent-a` 能被选择加入 | 改为候选 `agent-b` | 本版候选契约排除已在本组成员。 | 对实际 POST `/members` 的 204 和精确载荷仍断言。 |
| 500、坏 JSON、超时、空候选加载分支 | 保留；选择器从旧全局 `#group-status` 补充为贴近重试控件的 `#groupRetryFeedback`，空候选仍断言 `#group-status` 和禁用下拉 | 反馈位置随控件语义调整，状态机不得丢失。 | 500/shape/timeout/empty 的每个分支均继续在浏览器中触发；加载中、加载失败、没有自己的 Agent、恢复成功全部断言。 |

附加而非删减的保护：恢复迟到快照代次回归、恢复初始加载三态、采集
`pageerror` 与 console error、并使用真实键盘 `pressSequentially` 输入中文团队名。

本清单不能证明真实已登录用户或生产网络条件下的交互；它只证明由生产
`consolePageHTML` 直接抽取后，在受控 HTTP fixture 与真实 Chromium 中的 DOM 行为。
