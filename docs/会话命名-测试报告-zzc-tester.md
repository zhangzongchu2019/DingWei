# 会话命名强制规范 v1.0 测试报告

> 测试者: zzc-tester | 日期: 2026-07-09 | 被测分支: feat/session-naming-v1
>
> 规范依据: `dingwei/docs/会话命名强制规范-v1.0.md`
> 用例依据: `TASK-session-naming-v1.md` tester 节
>
> **测试基线**: `5fe5591` (修复发信临时会话命名) — 二轮验证
> 初轮基线: `388435b` (实现会话命名强制规范 v1.0)

## 一、测试概览

| 维度 | 用例数 | 通过 | 失败 | 状态 |
|------|--------|------|------|------|
| 客户端拒启动 | 6 | 6 | 0 | ✅ |
| 客户端正则验证 | 6 | 6 | 0 | ✅ |
| 客户端 P1: send.py note 名 | 4 | 4 | 0 | ✅ |
| 服务端 enforce 拒连 | 3 | 3 | 0 | ✅ |
| 服务端 enforce 合规 | 1 | 1 | 0 | ✅ |
| 服务端 warn 模式 | 1 | 1 | 0 | ✅ |
| 服务端 off 模式 | 1 | 1 | 0 | ✅ |
| 服务端灰度三档 | 3 | 3 | 0 | ✅ |
| 服务端 P1: send note 名 enforce | 3 | 3 | 0 | ✅ |
| 渲染: shortSessionName | 10 | 10 | 0 | ✅ |
| 渲染: 在线清单 | 5 | 5 | 0 | ✅ |
| 渲染: #bg bug | 1 | 1 | 0 | ✅ |
| 现有回归 | 65+全量 | 65+全量 | 0 | ✅ |

**总体: 全部通过 ✅ (含 P1 修复验证)**

---

## 二、客户端验证 (config.py / load_config)

### 2.1 拒启动 6 场景

| # | 场景 | env 输入 | 预期 | 实际结果 | 错误信息 |
|---|------|----------|------|----------|----------|
| 1 | 缺 SH_OWNER | `SH_OWNER` 未设置 | SystemExit | ✅ PASS | `SH_OWNER is required。` + 格式提示 + 本机示例 |
| 2 | 缺 SH_SESSION_NAME | `SH_SESSION_NAME` 未设置 | SystemExit | ✅ PASS | `SH_SESSION_NAME is required。` + 格式提示 + 本机示例 |
| 3 | 短名含大写 | `Tester` | SystemExit | ✅ PASS | `SH_SESSION_NAME 不合规: 短名只能小写字母数字, 如 manager。` + 格式+示例 |
| 4 | 短名含 `-` | `my-app` | SystemExit | ✅ PASS | `SH_SESSION_NAME 不合规: 短名只能小写字母数字, 如 manager。` + 格式+示例 |
| 5 | 短名含空格 | `my app` | SystemExit | ✅ PASS | `SH_SESSION_NAME 不合规: 短名只能小写字母数字, 如 manager。` + 格式+示例 |
| 6 | SH_OWNER 含大写 | `Zzc` | SystemExit | ✅ PASS | `SH_OWNER 不合规: 只能使用小写字母数字。` + 格式+示例 |

**验证要点**:
- ✅ 所有场景均抛出 `SystemExit`（拒绝启动，绝不带病连接）
- ✅ 错误信息包含"正确格式"说明 + 本机示例
- ✅ 三种非法类型分别给出针对性提示（缺 env / 短名非法 / owner 非法）

### 2.2 合规场景

| # | 场景 | env 输入 | 预期 session_name | 实际 | 状态 |
|---|------|----------|-------------------|------|------|
| 7 | 合规名 | `SH_OWNER=zzc, SH_SESSION_NAME=tester, SH_KEY_ID=FB-zzc-devteam-e0d12642` | `zzc-tester-2642` | `zzc-tester-2642` | ✅ |

### 2.3 底层正则验证

| 正则 | 测试项 | 结果 |
|------|--------|------|
| `NAME_PART_RE` | `abc123` / `tester` / `dev1013` accepted | ✅ |
| `NAME_PART_RE` | `my-app` rejected (含 `-`) | ✅ |
| `NAME_PART_RE` | `Tester` rejected (含大写) | ✅ |
| `NAME_PART_RE` | `my app` rejected (含空格) | ✅ |
| `SESSION_NAME_RE` | `zzc-tester-2642` / `fulei-dev1013-3dd6` / `abc123-def456-1a2b` accepted | ✅ |
| `SESSION_NAME_RE` | `developer` / `dev1013` rejected (无三段式) | ✅ |

### 2.4 现有测试回归

- `test_load_config_required_and_defaults`: ✅
- `test_load_config_requires_owner_and_valid_short_name`: ✅
- Python 全量 64/64: ✅

---

## 三、服务端验证 (hub.go /ws/session)

### 3.1 enforce 模式拒连 3 场景 (`TestSessionNameEnforceRejectsInvalidNames`)

| # | 场景 | 会话名 | 预期 | 实际 | 状态 |
|---|------|--------|------|------|------|
| 1 | 非法正则 | `developer` | 400 + "会话名不合规" | 400 + "会话名不合规,须为 `<owner_key>-<短名>-<key末4位>`,如 fulei-dev1013-3dd6" | ✅ |
| 2 | 末4位不符 | `u1-developer-0000` | 400 + "末4位" | 400 + "会话名末4位与 SH_KEY_ID 不匹配" | ✅ |
| 3 | owner_key 不符 | `u2-developer-<tail>` | 400 + "owner_key" | 400 + "会话名 owner_key 与该 key 绑定成员不匹配" | ✅ |

### 3.2 enforce 模式合规连接

| # | 场景 | 会话名 | 预期 | 实际 | 状态 |
|---|------|--------|------|------|------|
| 4 | 合规三段式 | `u1-developer-<keyTail>` | 连接成功, 入 sessionClients | WebSocket 连接成功, `waitSessionOnline` 通过 | ✅ |

### 3.3 交叉 owner 拒绝 (`TestSessionNameEnforceCrossOwnerRejection`)

| 场景 | 预期 | 状态 |
|------|------|------|
| u2 的 owner_key + u1 的 key | 400 + "owner_key" | ✅ |

### 3.4 灰度三档 (`TestGrayscaleSwitchThreeModes`)

| 模式 | 非法名 `dev1013` | 预期行为 | 实际 | 状态 |
|------|-----------------|----------|------|------|
| `off` | 连接 | 放行, 无校验 | WebSocket 连接成功 | ✅ |
| `warn` | 连接 | 放行 + 日志记录 + 目录告警 | 连接成功, 日志输出 "session name warning...", 目录含 `命名告警:` | ✅ |
| `enforce` | 连接 | 拒绝 400 | 400 + "会话名不合规" | ✅ |

### 3.5 补充服务端测试

| 测试 | 内容 | 状态 |
|------|------|------|
| `TestSessionNameCompliantRegistersAndAppearsInDirectory` | 合规名注册 + 在线清单渲染完整验证 | ✅ |
| `TestSessionNameWarnModeKeepsOnlineButLogs` | warn 模式端到端: 非法名放行 + 告警 | ✅ |
| `TestSessionNameOffModeNoValidation` | off 模式端到端: 任意名放行 + 无告警 | ✅ |
| `TestSessionNamePolicyWarningEdgeCases` | policyWarning 5 边界: 合规/正则/末4位/owner/空owner | ✅ |
| `TestSessionNameAutoSuffixWithinOwner` | 同 owner 重复名自动后缀 | ✅ |
| `TestSessionNameReconnectKeepsNameAndReleaseReuses` | 重连保持名 + 释放重用 | ✅ |

### 3.6 现有 Go 全量回归

- `go test ./internal/m8/` 全量: ✅ PASS (21s)

---

## 四、渲染验证

### 4.1 shortSessionName 三段式切分

| 输入 | 期望输出 | 实际 | 状态 |
|------|----------|------|------|
| `zzc-manager-2642` | `manager` | `manager` | ✅ |
| `fulei-dev1013-3dd6` | `dev1013` | `dev1013` | ✅ |
| `abc-def-1a2b` | `def` | `def` | ✅ |
| `dev1013` (老脏名) | `dev1013` | `dev1013` | ✅ |
| `sh-developer-e0d12642` (旧格式) | `developer` | `developer` | ✅ |
| `sh-something-1234` (旧格式) | `something` | `something` | ✅ |
| `trailing-` (非法) | `trailing-` | `trailing-` | ✅ |
| `""` (空) | `""` | `""` | ✅ |

### 4.2 #bg bug 不复现

| 验证项 | 详情 | 状态 |
|--------|------|------|
| 老脏名 `dev1013` 显示 `#dev1013` | ✅ 出现在在线清单中 | ✅ |
| `#bg` 不出现在清单中 | 旧 bug: 从后截取 `dev1013` → `#bg` | ✅ |
| 寻址 `@u1#dev1013` 正确 | 基于真实 `ep.SessionName` | ✅ |

### 4.3 合规名在线清单渲染

输入: 合规名 `u1-manager-2642` (warn 模式, webTerminal=true, terminalViewBase set)

| 渲染元素 | 期望 | 实际 | 状态 |
|----------|------|------|------|
| `#短名` | `#manager` | `#manager` | ✅ |
| `@owner#短名` | `@u1#manager` | `@u1#manager` | ✅ |
| `全名:` | `全名:u1-manager-2642` | `全名:u1-manager-2642` | ✅ |
| `/view/` URL | `/view/u1-manager-2642` | `/view/u1-manager-2642` | ✅ |
| `命名告警:` | 不出现 | 不出现 | ✅ |

### 4.4 warn 模式脏名在线清单渲染

输入: 老脏名 `dev1013` (warn 模式, webTerminal=true)

| 渲染元素 | 期望 | 状态 |
|----------|------|------|
| `#dev1013` | 显示 (基于 ep.SessionName) | ✅ |
| `@u1#dev1013` | 寻址正确 | ✅ |
| `/view/dev1013` | 路由可达 | ✅ |
| `命名告警:` | 出现 (warn 模式) | ✅ |
| `#bg` | 不出现 | ✅ |

---

## 五、P1 修复验证: send_dingwei.py 临时发信名 (基线 5fe5591)

### 5.1 缺陷描述 (初轮 388435b)

`send_dingwei.py` 原用 `name = f"{me}-note"` 构造临时发信会话名（`me` 取自 `SH_SESSION_NAME`）。在 enforce 模式下，产生的 `manager-note` / `sender-note` 不匹配三段式正则 `^[a-z0-9]+-[a-z0-9]+-[0-9a-f]{4}$`，导致 WebSocket 握手被 Hub 拒绝（400）。

### 5.2 修复方案

新增 `temporary_session_name()` 函数，基于 `SH_OWNER` + `SH_SESSION_NAME` + `SH_KEY_ID` 末4位构造合规三段式临时发信名 `<owner>-<short>note-<tail>`。兼容新旧两种 SH_SESSION_NAME 格式：

- 新格式短名（如 `tester`）：直接使用
- 旧格式三段式（如 `zzc-manager-2642`）：反向解析出 owner 和 short
- 无 SH_OWNER 时回退到 `zzc`
- 无效短名时回退到 `sender`

### 5.3 验证结果

**Go 服务端 enforce 测试** (`TestSessionNameEnforceAllowsSendNoteName` / `TestSendNoteNameVariants` / `TestSessionNamePolicyWarningAllowsNoteSuffix`):

| 场景 | 输入 | 预期 | 状态 |
|------|------|------|------|
| enforce: 合规 note 名 | `u1-sendernote-<tail>` | 连接成功, 入 sessionClients | ✅ |
| enforce: 老式 note 名 | `sender-note` | 400 + "不合规" | ✅ |
| enforce: manager note | `u1-managernote-<tail>` | 连接成功 | ✅ |
| enforce: tester note | `u1-testernote-<tail>` | 连接成功 | ✅ |
| policyWarning: 合规 note | `u1-sendernote-a1b2` | 无告警 | ✅ |
| policyWarning: 老式 note | `sender-note` | "不合规" | ✅ |

**Python 客户端测试** (`test_30~33`):

| 场景 | 状态 |
|------|------|
| `<owner>-<short>note-<tail>` 通过 SESSION_NAME_RE | ✅ |
| 老式 `sender-note` 不通过 SESSION_NAME_RE | ✅ |
| `temporary_session_name()` 正确构造 | ✅ |

**temporary_session_name() 兼容性实测**:

| env 输入 | 输出 | 合规 |
|----------|------|------|
| `SH_OWNER=zzc, SH_SESSION_NAME=tester, KEY=FB-...2642` | `zzc-testernote-2642` | ✅ |
| `SH_OWNER=zzc, SH_SESSION_NAME=zzc-manager-2642` (三段式) | `zzc-managernote-a1b2` | ✅ |
| 无 SH_OWNER, `SH_SESSION_NAME=sender` | `zzc-sendernote-a1b2` | ✅ |
| 旧规范 `SH_SESSION_NAME=dev1013` | `zzc-dev1013note-3dd6` | ✅ |
| 短 key `FB` | `u-xnote-0000` | ✅ |

---

## 六、实现符合性对照

| 规范条款 | 实现位置 | 验证方式 | 结论 |
|----------|----------|----------|------|
| 一、命名格式 `^[a-z0-9]+-[a-z0-9]+-[0-9a-f]{4}$` | hub.go L144 `sessionNamePattern` + config.py L16 `SESSION_NAME_RE` | 正则测试 | ✅ |
| 一、owner_key 语义校验 | hub.go L3122-3123 `sessionNamePolicyWarning` | enforce 测试 | ✅ |
| 一、末4位语义校验 | hub.go L3116-3117 `sessionNamePolicyWarning` | enforce 测试 | ✅ |
| 二、自动拼装 | config.py L168-193 `build_session_name` | 合规名测试 | ✅ |
| 三、客户端缺 env 拒启动 | config.py L157-165 `required_session_env` | 客户端拒启动测试 | ✅ |
| 三、短名非法拒启动 | config.py L180-185 | 客户端拒启动测试 | ✅ |
| 三、SH_OWNER 非法拒启动 | config.py L174-179 | 客户端拒启动测试 | ✅ |
| 三、最终正则复核 | config.py L186-192 | 合规名测试 | ✅ |
| 四、正则拒连 | hub.go L668-672 | enforce 测试 | ✅ |
| 四、末4位拒连 | hub.go L3116-3117 | enforce 测试 | ✅ |
| 四、owner_key 拒连 | hub.go L3122-3123 | enforce 测试 | ✅ |
| 四、合规注册 | hub.go L678-693 `registerSessionEndpoint` | enforce 合规测试 | ✅ |
| 五、寻址基于真实 ep.SessionName | hub.go L3051 `short := shortSessionName(ep.SessionName)` | 渲染测试 | ✅ |
| 五、shortSessionName 三段式切分 | hub.go L3082-3092 | 渲染测试 | ✅ |
| 五、全名展示 | hub.go L3059-3061 | 渲染测试 | ✅ |
| 五、view 路由用完整名 | hub.go L3057 `url.PathEscape(ep.SessionName)` | 渲染测试 | ✅ |
| 六、灰度开关 WP_NAME_ENFORCE | hub.go L3094-3102 `sessionNameEnforceMode` | 灰度三档测试 | ✅ |
| 六、off 不校验 | hub.go L667 case ""/default → warn; off 显式跳过 | off 测试 | ✅ |
| 六、warn 放行+记录 | hub.go L673-676 | warn 测试 | ✅ |
| 六、enforce 硬拒 | hub.go L668-672 | enforce 测试 | ✅ |

---

## 七、测试环境

- 分支: `feat/session-naming-v1`
- 测试基线: `5fe5591` (修复发信临时会话命名) — 二轮验证
- 初轮基线: `388435b` (实现会话命名强制规范 v1.0)
- Python: 3.12.3, Go: 1.23.4, Linux 6.8.0-124-generic
- 测试脚本:
  - `tools/sessionhelper/tests/test_session_naming_client.py` (18 用例: 14 初轮 + 4 P1)
  - `internal/m8/hub_session_naming_supplement_test.go` (12 用例: 9 初轮 + 3 P1)
  - 现有 `test_sessionhelper.py` (65 用例, 全量回归)
  - 现有 `hub_test.go` (命名相关 6 用例 + 全量回归)

## 八、结论

**全部测试通过 ✅ (二轮验证, 含 P1 修复)**。会话命名强制规范 v1.0 双端实现符合规范要求:

1. **客户端**: 6 种非法输入均正确拒绝 (SystemExit)，错误信息包含正确格式 + 本机示例；合规名正确自动拼装
2. **服务端**: enforce 模式正确拒绝非法正则/末4位不符/owner_key 不符；warn 模式放行+告警；off 模式不校验
3. **渲染**: shortSessionName 正确切三段式中段，老脏名 #bg bug 不复现，全名/路由均基于完整注册名
4. **灰度**: `WP_NAME_ENFORCE=off|warn|enforce` 三档切换生效，默认 warn
5. **P1 修复**: `send_dingwei.py` 临时发信名现已构造为合规 `<owner>-<short>note-<tail>` 格式，enforce 模式下正常通过 Hub 校验；兼容新旧 SH_SESSION_NAME 格式与无 SH_OWNER 回退

建议可以进入 manager 终评阶段。
