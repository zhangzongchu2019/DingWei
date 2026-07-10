# feishu-sync (#sync) 测试计划 · zzc-tester

> 测试对象: feat/feishu-sync@312fbb2 (飞书按需镜像同步)
> 测试依据: 派工 6 项验收点
> 测试平台: Linux (美洲机) + macOS (zzc-mac)
> 测试日期: 2026-07-08

## 实现概要 (代码审查)

**变更**: hub.go (+176), hub_test.go (+200), terminal.go (+6)
**机制**:
- Hub 缓存每会话最近 10 条终端输出 (UTC 时间戳)
- `#sync <会话>` → 补发最近 10 条 + 开启实时同步
- `#unsync <会话>` → 停止同步
- 跨用户需显式 key: `#sync <会话>#<key>`
- 多人独立同步 (按 botChannelID:openID 区分)
- 会话下线 → 清缓存 + 清同步目标

**Go 测试**: 2 个测试函数 (回填+实时+UTC+unsync+缓存清理 / 跨用户+显式key)

---

## 前置条件

- [ ] manager 评审通过 feat/feishu-sync@312fbb2
- [ ] 线上部署新代码 + 重启 dingwei.service
- [ ] zzc-tester (Linux) sessionHelper 在线，已开 /view 页面可观察终端输出
- [ ] zzc-mac (macOS) sessionHelper 在线
- [ ] 飞书可向机器人发消息 (personal chat)

---

## 测试用例

### 用例 ①: 默认无镜像 (会话 I/O 不进飞书)

**前置**: 未对任何会话执行 #sync

**步骤**:
1. zzc-tester 在 CLI 中产生终端输出 (如 echo "test-default-no-sync")
2. 检查飞书 personal chat 是否收到该输出

**预期**:
- ✅ 飞书**不收到**会话终端输出 (默认无镜像)
- ✅ /view 页面正常显示输出

**Linux 结果**: [ ] PASS / [ ] FAIL
**macOS 结果**: [ ] PASS / [ ] FAIL

---

### 用例 ②: #sync 开启 → 收到最近 10 条历史 + 实时

**步骤**:
1. zzc-tester 终端产生多条输出: "sync-test-line-01" ~ "sync-test-line-15"
2. 在飞书 personal chat 发: `#sync zzc-tester`
3. 观察飞书收到的消息

**预期**:
- ✅ 飞书回复: "已开启同步 zzc-tester"
- ✅ 收到最近 10 条终端输出 (line-06 ~ line-15)，每条带 `[YYYY-MM-DD HH:MM:SS UTC]` 前缀
- ✅ 之后在 zzc-tester CLI 产生新输出，飞书实时收到

**Linux 结果**: [ ] PASS / [ ] FAIL
**macOS 结果**: [ ] PASS / [ ] FAIL (对 zzc-mac 发 `#sync zzc-mac`)

---

### 用例 ③: #unsync → 停止同步

**步骤**:
1. 在用例②基础上，飞书发 `#unsync zzc-tester`
2. 观察飞书回复
3. zzc-tester CLI 再产生新输出: "after-unsync-test"

**预期**:
- ✅ 飞书回复: "已停止同步 zzc-tester"
- ✅ "after-unsync-test" **不出现在**飞书消息中

**Linux 结果**: [ ] PASS / [ ] FAIL
**macOS 结果**: [ ] PASS / [ ] FAIL

---

### 用例 ④: 跨 owner 鉴权 — 无 key 拒 + 显式 key 也拒 🔴

> 🔴 manager 评审硬伤 #1: 原实现显式 key 不校验 owner 归属，key_id 非秘密 → 任意租户成员可旁观他人终端。
> 修复后预期: 跨 owner 显式 key 也被拒。

**步骤 ④a — 无 key 跨 owner**:
1. 飞书发 `#sync <不属于自己的会话>`（不带 #key）

**预期 ④a**:
- ✅ 回复: "未找到你名下在线会话：<会话名>。同步他人会话请显式使用 #sync <会话名>#<key>。"

**步骤 ④b — 显式 key 但跨 owner** 🔴:
1. 飞书发 `#sync <他人会话>#<他人key_id>`
2. 该 key 属于不同 owner

**预期 ④b**:
- ✅ 回复: "无权同步他人会话" 或类似拒绝消息
- ✅ 不会开启同步

**结果**: [ ] PASS / [ ] FAIL

---

### 用例 ⑤: 多人同步同一会话互不影响

**步骤**:
1. 飞书账号 A 发 `#sync zzc-tester`
2. 飞书账号 B 也发 `#sync zzc-tester` (需显式 key: `#sync zzc-tester#FB-zzc-devteam-e0d12642`)
3. zzc-tester 产生输出
4. 飞书 A 发 `#unsync zzc-tester`
5. zzc-tester 再产生输出

**预期**:
- ✅ A 和 B 都收到实时输出
- ✅ A unsync 后 A 不再收到，但 B 仍然收到
- ✅ 互不影响

**结果**: [ ] PASS / [ ] FAIL

---

### 用例 ⑥: 会话下线 → 缓存清理

**步骤**:
1. 开启 #sync zzc-tester
2. 产生几条终端输出确认缓存存在
3. 主动断开 zzc-tester 的 sessionHelper (kill 或退出)
4. 重启 sessionHelper 重连
5. 开启 #sync zzc-tester

**预期**:
- ✅ sessionHelper 下线后，Hub 缓存被清理
- ✅ 重新 #sync 后，**只补发新会话的输出** (旧会话的输出已清理)
- ✅ 或新会话无历史输出，收到空的/无历史补发

**结果**: [ ] PASS / [ ] FAIL

---

### 用例 ⑦: 同步内容无 ANSI 乱码 + 不刷屏 (限流) 🔴

> 🔴 manager 评审硬伤 #2: PTY 裸流转发导致 ANSI 转义序列乱码，且 TUI spinner 等高频输出会轰炸飞书。

**步骤**:
1. 开启 #sync zzc-tester
2. zzc-tester 执行产生 ANSI 转义序列的命令 (如 `ls --color`, 或含 spinner 的 TUI 程序)
3. 观察飞书收到的消息内容
4. 统计 1 分钟内飞书收到的消息条数

**预期**:
- ✅ 飞书收到的文本**不含** ANSI 转义序列 (如 `\033[...m`、`\e[...` 等)
- ✅ 非空有意义内容的 chunk 才发送
- ✅ spinner 空转/高频刷新时飞书**不刷屏** (每分钟消息量有合理上限，如 ≤20 条/分钟)

**Linux 结果**: [ ] PASS / [ ] FAIL
**macOS 结果**: [ ] PASS / [ ] FAIL

---

### 用例 ⑧: 空白/纯控制序列 chunk 不发送

**步骤**:
1. 开启 #sync zzc-tester
2. zzc-tester 只产生空白字符或纯 ANSI 控制序列的输出
3. 观察飞书是否收到消息

**预期**:
- ✅ 纯空白 chunk (全空格/换行/制表) 不触发飞书消息
- ✅ 纯 ANSI 控制序列 (无可见文本) 不触发飞书消息
- ✅ 只有包含可见文本的 chunk 才发送

**Linux 结果**: [ ] PASS / [ ] FAIL
**macOS 结果**: [ ] PASS / [ ] FAIL

| 用例 | Linux (zzc-tester) | macOS (zzc-mac) | 备注 |
|------|-------------------|-----------------|------|
| ① 默认无镜像 | [ ] | [ ] | |
| ② #sync 历史+实时 | [ ] | [ ] | |
| ③ #unsync 停止 | [ ] | [ ] | |
| ④ 跨owner鉴权(无key+显式key) | [ ] | N/A | 🔴 服务端逻辑 |
| ⑤ 多人独立 | [ ] | N/A | 需多飞书账号 |
| ⑥ 下线清理 | [ ] | [ ] | |
| ⑦ ANSI乱码+限流防刷屏 | [ ] | [ ] | 🔴 评审硬伤#2 |
| ⑧ 空白/控制序列过滤 | [ ] | [ ] | |

## 前置阻塞

⏳ 等待 developer 修复 2 条硬伤 + manager 复审通过:
1. 🔴 跨 owner 显式 key 鉴权 (补 owner 校验)
2. 🔴 ANSI 转义过滤 + 限流 (防乱码 + 防刷屏)
