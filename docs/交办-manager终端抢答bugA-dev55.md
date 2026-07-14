# 交办 dev55：bug A —— 会话网页终端里输入 `#别的会话 X`，被本地 CLI 也消费（抢答）

- 派发：manager（zzc-manager-2642），2026-07-12
- 性质：**先复现定位，再修**（manager 未 100% 确认分叉点，任务含调查）
- 负责：dev55；评审+部署：manager（禁碰生产）；提交身份 zhangzongchu2019 <tchangshark@outlook.com>

## 现象（manager 亲历，真机可复现）

用户通过 **manager 会话的网页终端 `https://dingwei.wegoab.com/view/zzc-manager-2642`** 输入 `#tester 向facebook发起request...` / `#tester 1`…`#tester 10`。结果：

1. ✅ **正确路由到 tester**：投递表铁证，10 条全部 `to=zzc-tester-2642`（tester 串行处理、零 dead）。
2. ❌ **但同一条输入也被 manager 自己的 CLI 消费了**：manager CLI（一个 claude 会话）收到了 `#tester` 后面的**正文**（`#tester` 前缀被剥掉），当成给自己的命令**执行并回复**，回复又被当成 `【manager】` 发回用户飞书 → 用户看到 `#tester` 消息被 manager「抢答」。

**即：`#别的会话 X` 作为一个会话的终端输入时，被"路由给目标"和"喂给本地 CLI"双重消费了。应只路由、不喂本地。**

## 期望行为

在**任一会话**的终端里输入 `#其它会话名 正文`（或 `@成员#会话 正文`）：
- **只**把消息路由到目标会话；
- **本地 CLI 不应再收到这条正文、不应据此产生回复**。
- 反之，不带 `#选择器` 的普通输入，才是给本地 CLI 的（保持现状）。

## 已知代码坐标（供定位，未确认分叉点）

- helper：`tools/sessionhelper/sessionhelper/app.py`
  - `LEGACY_AGENT_ROUTE_RE`（:58）匹配 `^#名字\s+正文` / `@成员#会话\s+正文`；`:122` 处 match。
  - `CONTROL_ROUTE_RE`（:59）匹配 `[[DW_ROUTE_V1 ...]]` 控制块。
  - 需确认：这个 route 正则扫的是 CLI 的**输出流**（AI 生成的发信）还是**也包括注入进去的输入回显**？网页终端输入注入到 PTY 后，回显是否被当成"AI 要发的 #路由"截获、同时又留在 CLI 输入里被处理？
- Hub：`internal/m8/hub.go`
  - `routeSessionSelectorEnvelope`（:2616，调用点 :876 在 HandleSessionWS 消息循环）——会话发来的 `#选择器` envelope 在此路由。
  - 网页终端输入路径：`/ws/view/`（terminal.go），view 输入经 Hub 转发注入到会话 helper 的 CLI PTY。
- 关键调查点：**用户在 manager view 打的 `#tester X`，到底在哪一层被"复制"成了两份**（一份路由 tester、一份进 manager CLI）？是 helper 把 PTY 输入既注入又按 route 正则截发？还是 Hub 的 view-input 与 selector-route 两条路都跑了？

## 修复方向（定位后再定，参考）

- 若是 **helper 层**：注入进 CLI 的输入里，若整行是 `#其它会话 正文` 选择器，应**路由后不再作为 CLI prompt**（或路由与本地消费二选一，选择器命中→只路由）。
- 若是 **Hub view-input 层**：view 输入若是 `#选择器`，应只做选择器路由、不再 echo/inject 给源会话 CLI。
- 注意区分：`#自己会话名` 或无选择器 → 正常给本地 CLI；`#别的会话` → 只路由。

## 验收判据（manager 真机复测）

- [ ] 在 `/view/zzc-manager-2642` 输入 `#tester 你好`：tester 收到并（可）回复；**manager CLI 不产生任何回复、不执行**。
- [ ] 在 manager view 输入普通文字（无 `#`）：manager CLI 正常处理（不回归）。
- [ ] 会话间用 `#名字`/`DW_ROUTE` 正常发信不回归（对方收到，发起方本地不重复消费）。
- [ ] go build/test 全绿；helper 单测覆盖"选择器输入只路由不喂本地"。

## 交付

**先回执你的复现结果 + 确认分叉点在哪层**，再动手改。独立 commit + 说明 → send.py 回报 **zzc-manager-2642**（用全名）→ manager 评审+部署。分支别 push，禁碰生产。
