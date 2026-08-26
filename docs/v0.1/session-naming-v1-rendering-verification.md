# 会话命名 v1.0 渲染修正验证方案

## 验证维度

### 1. shortSessionName 三段式切分

**已覆盖测试**: `TestShortSessionNameUsesThreePartRegisteredName` + `TestShortSessionNameEdgeCases`

| 输入 | 期望输出 | 说明 |
|------|----------|------|
| `zzc-manager-2642` | `manager` | 合规三段式 → 切中段 |
| `fulei-dev1013-3dd6` | `dev1013` | 合规三段式 |
| `dev1013` | `dev1013` | 老脏名 → 原样返回 (不切) |
| `sh-developer-e0d12642` | `developer` | 旧 sh- 格式兼容 |
| `zzc-a-b` | `zzc-a-b` | 非规范格式 → 原样返回 |
| `""` | `""` | 空字符串 |

### 2. #bg bug 不复现

**已覆盖测试**: `TestSessionNameWarnAllowsLegacyNameAndDirectoryFlagsIt`

- 输入: 老脏名 `dev1013`
- 验证: `#dev1013` 出现在在线清单中
- 验证: `#bg` 不出现在在线清单中 (旧 bug: 从后截取 `#bg`)
- 寻址地址: `@u1#dev1013` 可用

### 3. 合规名渲染: #短名 / 全名 / view URL

**已覆盖测试**: `TestSessionNameCompliantDirRendering` + `TestSessionNameCompliantRegistersAndAppearsInDirectory`

| 渲染元素 | 合规名示例 | 期望 |
|----------|-----------|------|
| `#短名` | `#manager` | shortSessionName 切出中段 |
| `@owner#短名` | `@u1#manager` | 寻址基于真实 ep.SessionName |
| 全名: | `全名:u1-manager-2642` | SessionName != short 时展示 |
| /view/ 路径 | `/view/u1-manager-2642` | 用完整注册名保证可达 |
| 命名告警 | 不出现 | 合规名无告警 |

### 4. 灰度三档在线清单表现

| 模式 | 非法名连接 | 在线清单告警 | 说明 |
|------|-----------|-------------|------|
| `off` | ✅ 放行 | ❌ 无告警 | 完全不校验 |
| `warn` (默认) | ✅ 放行 | ✅ `命名告警:` 具体原因 | 记录但放行 |
| `enforce` | ❌ 拒绝 (400) | (不会进入清单) | 硬拒绝 |

### 5. 手动验证流程 (developer 提测后执行)

```bash
# 1. 启动 Hub (warn 模式)
WP_NAME_ENFORCE=warn go run ./cmd/hub/...

# 2. 连接合规名客户端
SH_OWNER=zzc SH_SESSION_NAME=manager SH_KEY_ID=<key> SH_SECRET=<secret> \
  python3 tools/sessionhelper/sessionhelper.py

# 3. 连接非法名客户端 (模拟老客户端)
# 通过直接 WebSocket 连接传非法会话名

# 4. 查看在线清单 → 验证 #短名 正确、告警标记正确
# 向任意会话发送 #在线 命令获取清单文本
```

## 运行自动化测试

```bash
# Go 测试 (全部)
go test ./internal/m8/ -run "TestSessionName|TestShortSessionName" -v -count=1

# Go 测试 (补充用例)
go test ./internal/m8/ -run "TestSessionNameCompliant|TestSessionNameEnforceCross|TestSessionNameWarnMode|TestSessionNameOffMode|TestShortSessionNameEdge|TestSessionNamePolicyWarning|TestSessionNameCompliantDir|TestGrayscaleSwitch" -v -count=1

# Python 客户端测试
PYTHONPATH=tools/sessionhelper python3 tools/sessionhelper/tests/test_session_naming_client.py -v
```
