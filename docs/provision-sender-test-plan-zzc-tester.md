# provision 发送方测试计划 · zzc-tester

> 测试对象: feat/provision-sender (Hub.SendProvision + /admin/provision + 制品打包 + fleet 灰度)
> 测试依据: sessionHelper-provision-发送方-spec.md v0.1
> 测试平台: Linux(美洲机) + macOS(zzc-mac)
> 测试日期: 2026-07-08

## 前置条件

- [ ] developer 已完成 feat/provision-sender 服务端实现并部署到 ts.wegoab.com
- [ ] zzc-tester (Linux 美洲机) sessionHelper 在线
- [ ] zzc-mac (macOS) sessionHelper 在线
- [ ] Admin 后台 /admin/provision 可访问
- [ ] /dl/ 目录可上传制品

---

## 测试用例

### 用例 1: install_skill 正常下发 (sha256 正确)

**前置**: 准备一个 skill tar 包，上传到 /dl/，记录 sha256

**步骤**:
1. Admin 后台选择目标会话 (zzc-tester)，action=install_skill
2. 填入 skill 的 url、sha256、target (如 "test-skill")、version (如 "1.0.0")
3. 提交发送

**预期结果**:
- ✅ Admin 页面显示"发送成功"
- ✅ 目标客户端收到 provision 信封并下载安装
- ✅ skill 出现在 ~/.claude/skills/test-skill/ 目录
- ✅ 客户端回执 ProvisionResult{action:install_skill, target:test-skill, ok:true}
- ✅ 审计表 (provision_audit) 有记录: action=install_skill, ok=1
- ✅ Admin 收集到回执并展示

**Linux 结果**: [ ] PASS / [ ] FAIL
**macOS 结果**: [ ] PASS / [ ] FAIL

---

### 用例 2: install_skill sha256 错误 → 拒绝

**前置**: 使用用例 1 的同一 skill 包

**步骤**:
1. Admin 后台选择目标会话，action=install_skill
2. 填入正确的 url，但 **sha256 故意填错** (改最后一位)
3. 提交发送

**预期结果**:
- ✅ 服务端正常发出信封
- ✅ 客户端下载完成后 sha256 校验不匹配
- ✅ 客户端 **拒绝安装**，不落盘
- ✅ 客户端回执 ProvisionResult{ok:false, message 包含 "sha256 mismatch"}
- ✅ 审计记录 ok=0

**Linux 结果**: [ ] PASS / [ ] FAIL
**macOS 结果**: [ ] PASS / [ ] FAIL

---

### 用例 3a: update_self 正常升级 (版本更高 + sha256 正确)

**前置**: 
- 打包当前版本+1 的 sessionHelper (如 2.1.0 → 2.1.1)
- 上传到 /dl/sessionhelper-2.1.1.tar.gz，记录 sha256
- 目标客户端运行中

**步骤**:
1. Admin 后台选择目标会话，action=update_self
2. 填入 url、sha256、version="2.1.1"
3. 提交发送

**预期结果**:
- ✅ 客户端收到 provision → 下载 → sha256 校验通过
- ✅ 版本比较: 2.1.1 > 当前版本 → 允许升级
- ✅ 备份当前代码 → 解压新版 → 写 update_state → os._exit(0)
- ✅ guard/launchd 拉起新版 sessionHelper
- ✅ 新版成功连回 DingWei → confirm_update_connected()
- ✅ 新版回执 version=2.1.1

**Linux 结果**: [ ] PASS / [ ] FAIL
**macOS 结果**: [ ] PASS / [ ] FAIL

---

### 用例 3b: update_self 新版起不来 → 回滚旧版

**前置**: 打包一个**故意坏的** sessionHelper (如入口抛异常)
- 版本号设为 2.1.2
- 上传到 /dl/，记录 sha256

**步骤**:
1. Admin 后台对目标会话发 update_self version="2.1.2"
2. 客户端安装"坏"版本 → 重启
3. 等待 30 秒 (rollback timeout)

**预期结果**:
- ✅ 客户端安装后重启，但新版起不来/连不回 DingWei
- ✅ guard/launchd 拉起后，rollback_stale_update_if_needed() 检测超时
- ✅ 自动回滚: restore_backup(旧版) → 重启旧版
- ✅ 旧版连回 DingWei 正常工作
- ✅ update_state 记录 status="rolled_back"
- ✅ 审计记录 ok=0 (或标记为 rolled_back)

**Linux 结果**: [ ] PASS / [ ] FAIL
**macOS 结果**: [ ] PASS / [ ] FAIL

---

### 用例 4: 防降级 — 发低版本 update_self 被拒

**前置**: 目标客户端当前运行版本 2.1.0

**步骤**:
1. Admin 后台对目标会话发 update_self version="2.0.0" (低于当前)
2. 或发 version="2.1.0" (等于当前)

**预期结果**:
- ✅ 客户端 compare_versions(当前, 下发) >= 0 → 拒绝
- ✅ 不下载、不安装
- ✅ 回执 ProvisionResult{ok:true, message:"current version is newer or equal"}
  (注意: 防降级在客户端是 ok=true 但 message 说明原因——这是现有客户端行为)

**Linux 结果**: [ ] PASS / [ ] FAIL
**macOS 结果**: [ ] PASS / [ ] FAIL

---

### 用例 5: 非 admin / 非 workpulse 来源 → 发不出/被拒

**步骤 5a — 非 admin 访问**:
1. 未登录状态直接访问 /admin/provision
2. 或使用非 admin 账号登录后访问

**预期 5a**:
- ✅ 被重定向到登录页，或返回 401/403

**步骤 5b — 非 workpulse 来源发 provision 信封**:
1. 构造一个 from ≠ "workpulse#<key>" 的 provision 信封
2. 尝试发给客户端

**预期 5b**:
- ✅ 客户端 validate_source() 拒绝: "provision source denied"
- ✅ (服务端侧) SendProvision 方法天然使用 from="workpulse#keyID" + system=true，其他来源无法通过 Hub 正常发

**步骤 5c — 服务端 url host 白名单校验**:
1. Admin 提交 provision，url 指向非白名单 host (如 https://evil.com/skill.tar.gz)

**预期 5c**:
- ✅ 服务端在发送前校验 url host，拒绝并提示 "url host not allowed"

**Linux 结果**: [ ] PASS / [ ] FAIL
**macOS 结果**: [ ] PASS / [ ] FAIL

---

### 用例 6: Fleet 灰度批量下发

**前置**: 至少 2 个同 owner 的在线会话 (如 zzc-tester + 另一个)

**步骤**:
1. Admin 选择 fleet 模式: 选择"某 owner 全部在线会话"
2. 选择灰度: "先发 1 台"
3. 提交 install_skill 或 update_self 指令

**预期结果**:
- ✅ 第 1 台收到并执行 → 回执 ok
- ✅ Admin 页面显示第 1 台结果
- ✅ 确认无误后，点"全量下发"
- ✅ 其余会话也收到并执行
- ✅ 每台回执独立收集展示
- ✅ 审计记录每台都有

**Linux 结果**: [ ] PASS / [ ] FAIL

---

### 用例 7: 制品上传 + 自动算 sha256

**步骤**:
1. Admin /admin/provision 页面上传制品文件
2. 服务端保存到 /dl/ 并自动计算 sha256

**预期结果**:
- ✅ 文件保存到 /dl/<filename>
- ✅ sha256 自动回填到表单
- ✅ url 自动生成为 https://ts.wegoab.com/dl/<filename>

**Linux 结果**: [ ] PASS / [ ] FAIL

---

### 用例 8: 目标会话离线 → 报错

**步骤**:
1. 确保目标会话离线
2. Admin 尝试对该会话发 provision

**预期结果**:
- ✅ 服务端返回错误: "session xxx offline" 或类似
- ✅ Admin 页面显示该会话发送失败

**Linux 结果**: [ ] PASS / [ ] FAIL

---

### 用例 9: install_skill 重复下发同一版本 (幂等)

**前置**: 已成功安装过 test-skill v1.0.0

**步骤**:
1. Admin 再次对同一会话发 install_skill (相同 target + version)

**预期结果**:
- ✅ 客户端 is_installed() 返回 True → 跳过下载安装
- ✅ 回执 ProvisionResult{ok:true, message:"already installed"}

**Linux 结果**: [ ] PASS / [ ] FAIL
**macOS 结果**: [ ] PASS / [ ] FAIL

---

## 测试矩阵

| 用例 | Linux (美洲机) | macOS (zzc-mac) | 说明 |
|------|---------------|-----------------|------|
| 1. install_skill 正常 | [ ] | [ ] | |
| 2. sha256 错误拒绝 | [ ] | [ ] | |
| 3a. update_self 升级 | [ ] | [ ] | Linux: cron+guard; macOS: launchd |
| 3b. update_self 回滚 | [ ] | [ ] | 自愈机制跨平台差异 |
| 4. 防降级拒绝 | [ ] | [ ] | |
| 5. 安全鉴权 | [ ] | [ ] | |
| 6. Fleet 灰度 | [ ] | N/A | 仅 Linux (需多会话) |
| 7. 制品上传 | [ ] | N/A | 服务端功能 |
| 8. 离线目标 | [ ] | [ ] | |
| 9. 幂等 | [ ] | [ ] | |

## 当前状态

**阻塞项**: 服务端 SendProvision 代码尚未实现。
**当前分支**: feat/provision-sender (仅含 spec 文档 bcbe68f)
**等待**: developer (zzc-developer) 完成 Hub.SendProvision + /admin/provision + 制品打包 + fleet 灰度 的实现

## 下一步

1. Developer 推送实现代码到 feat/provision-sender
2. git pull 拉取最新代码
3. 部署/重启服务端
4. 按本计划逐项测试
5. 汇总报告 → send.py 发给 zzc-manager
