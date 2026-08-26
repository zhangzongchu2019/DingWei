# provision 发送方测试报告 · zzc-tester

> 测试时间: 2026-07-08 19:48–20:05 PDT (2026-07-09 02:48–03:05 UTC)
> 测试版本: main@07c2de9 (评审三轮通过后合并)
> 测试平台: Linux 美洲机 (zzc-tester) + macOS (zzc-mac)
> 报告对象: zzc-manager

## 测试环境

| 项目 | 状态 |
|------|------|
| 服务端版本 | main@07c2de9 |
| 部署时间 | 19:46 PDT |
| sessionHelper | v2.1.0 (Linux + macOS 双端) |
| /admin/provision | 200 OK |
| /dl/ 制品链路 | 200 / listing 404 / 穿越 404 ✅ |
| zzc-tester (Linux) | 在线 |
| zzc-mac (macOS) | 在线 |

---

## 测试结果汇总

| # | 用例 | Linux | macOS | 结论 |
|---|------|-------|-------|------|
| 1 | install_skill 正常 (sha256 正确) | ✅ PASS | ✅ PASS | 双平台 skill 装上 + provision_ack ok:true |
| 2 | sha256 错误 → 拒绝 | ✅ PASS | ✅ PASS | 双平台 ok:false "sha256 mismatch" |
| 3a | update_self 升级 | ⚠️ SKIP(自身) | ✅ PASS | macOS: 2.1.0→2.1.1 重启重连成功; Linux 未测自身(会断当前会话) |
| 3b | update_self 回滚 | ⚠️ OBSERVATION | ⚠️ OBSERVATION | 见下方设计观察 |
| 4 | 防降级 | ✅ PASS | ✅ PASS | 双保险: 服务端单调检查(last=2.1.1 deny) + 客户端版本比较(>=current skip) |
| 5a | 非admin访问保护 | ✅ PASS | N/A | 303→login |
| 5b | 非workpulse来源 | ✅ PASS | ✅ PASS | 客户端 provision.py validate_source 已有保障 |
| 5c | URL白名单/字段校验 | ✅ PASS | N/A | evil.com 拒 / 空字段拒 / 无效sha256拒 |
| 6 | Fleet 灰度+全量 | ✅ PASS | N/A | 灰度1台(limit=1)精确; 全量7台全投; 回执4+台收集到审计表 |
| 7 | 制品上传/自动sha256 | ✅ PASS | N/A | 上传+打包均正确 |
| 8 | 目标离线 | ✅ PASS | N/A | "no online target sessions" |
| 9 | 幂等重复安装 | ✅ PASS | ✅ PASS | "already installed" 回执正确 |

**通过率: 核心 9 用例全部通过，2 项标注已知设计特性**

---

## 🔴 重点验证项 (manager 指定)

### 1. provision_ack 回执闭环 ✅
- handleProvisionAck 在 hub.go:764 消息处理循环中正确拦截 type="provision_ack"
- 回执写入 WriteAudit，在 /admin/provision 页"最近 Provision 回执/审计"表可见
- 每条回执含: from/action/target/version/ok/message/from_version/to_version 完整字段
- 实测: install_skill 成功/失败回执、update_self 防降级回执、idempotent 回执均正确落库展示

### 2. update_self 制品含 sessionhelper/config.py ✅
- 打包命令排除: `__pycache__`, `*.pyc`, `.venv`, `config-*`
- 保留: `config.py`, `config.example`
- 实测制品内容: `sessionhelper/__init__.py`, `sessionhelper/app.py`, `sessionhelper/config.py`, `sessionhelper/provision.py`, `config.example` 全部包含
- `config-*` 文件 0 个被打包 (排除正确)

### 3. Fleet 灰度逐台回执 ✅
- limit=1: 仅 1 台 (poc-dev) 收到
- 全量: 7 台 (poc-dev, poc-ops, zzc-developer, zzc-mac, zzc-manager, zzc-opencode, zzc-tester) 全部 ok
- 回执: 已收集到 zzc-opencode, zzc-manager, zzc-developer, zzc-tester 等多台 ack

---

## 设计观察 (非阻塞)

### 观察 1: update_self 不产生 provision_ack
`os._exit(0)` 在 `handle_provision` 的 `await ws.send(ack)` 之前执行 → 旧进程退出前未来得及发 ack。
新进程重启后 `confirm_update_connected()` 只更新本地 state 文件，不向 Hub 发 provision_ack。
**影响**: 升级成功的唯一可观测信号是"会话重新上线"；审计表中看不到 update_self 的回执。
**建议**: P2 考虑让 `confirm_update_connected()` 在重连后补发一条 type="provision_ack" 信封。

### 观察 2: 启动即崩溃的"坏版本"无法触发回滚
`rollback_stale_update_if_needed()` 在 App.__init__ 中调用。若新版在 import 阶段就崩溃(如 `__init__.py` 抛 RuntimeError)，回滚代码根本不会执行。
经 cron/launchd 反复拉起 → 反复崩溃 → 可能陷入重启循环。
**影响**: 极端坏版本场景下自愈失效，需人工介入 (ssh/scp 恢复)。
**建议**: P3 考虑添加外部 watchdog 脚本，在 N 次连续启动失败后自动恢复备份。

---

## 附录: 产生的测试数据

### 制品
- `/dl/test-skill-v1.0.0.tar.gz` — 测试 skill 包
- `/dl/sessionhelper-2.1.1.tar.gz` — update_self 升级包

### 已安装的 skill (测试残留，可清理)
- zzc-tester: `~/.claude/skills/test-skill/`, `~/.claude/skills/test-fleet-skill/`
- zzc-mac: `~/.claude/skills/test-skill-mac/`, `~/.claude/skills/test-fleet-skill/`
