# 设计：provision 作为全网自动升级 + 死锁远程解锁通道

- **版本**：v1
- **作者**：manager（zzc-manager-2642）
- **日期**：2026-07-13
- **动因**：`is_idle` 死锁等 helper 侧问题的修复都需要更新客户端；不能每次手动 ssh 一台台升级。张宗楚 2026-07-13 拍板：用服务器下发 + 客户端自动更新。

---

## 1. 核心结论

**用 provision 的 `update_self` 做全网自动升级 —— 不下发可执行脚本，只下发代码包。**

| 方案 | 安全性 | 结论 |
|---|---|---|
| **hub 下发「可执行 python 脚本」并由 helper 执行** | ❌ **通用远程代码执行（RCE）**。hub 被攻破或 key 泄露 = 全网 helper 一键沦陷 | **不采用** |
| **hub 下发「代码包」+ `update_self` 动作，更新逻辑在 helper 自己代码里** | ✅ 即使伪造 provision 消息，最多触发「更新到某个签名过的包」，跑不了任意代码 | **采用** |

**关键原则：hub 只下发数据，不下发指令。** 更新逻辑（备份/解包/重启/回滚）固化在 helper 的 `provision.py`，hub 无法改变它。

---

## 2. `update_self` 已具备的能力（`provision.py:151`，已核实）

```
if compare_versions(__version__, version) >= 0: return "已是最新"   # 幂等，不降级
backup = copytree(package_root)                                    # 备份自身
try:
    extract_package(artifact, package_root)                        # 解包覆盖
    write_update_state(version, backup); request_restart()         # 重启加载新代码
except Exception:
    restore_backup(backup); raise                                  # 失败自动回滚
```

**4 道安全闸（已核实）**：
1. **来源校验**：`from == workpulse#<本机 key_id>` 且 `meta.system == true`，否则 `provision source denied`。
2. **sha256 校验**：`download_and_verify(url, sha256)`，不匹配即拒。
3. **host 白名单**：`provision_allowed_hosts`（现为 `dingwei.wegoab.com`），非白名单 host `url host denied`。
4. **动作白名单**：只允许 `update_self` / `install_skill` / `install_mcp`，其余 `unsupported action`。
5. **🔴 不执行制品内脚本**：解包后**只替换代码文件 + 重启**，无 `subprocess`/`exec`/`eval` 去跑解包内容。**这是「不是 RCE」的根本保证 —— 任何修改都不得破坏这一条。**

---

## 3. 关键事实：`is_idle` 死锁挡不住 provision（已核实）

`recv_loop`（`app.py`）分支顺序：
```
:306  is_provision_envelope   → handle_provision   ← provision 在这里
:309  is_delivery             → accept_delivery
:334  buffer_if_busy          → 进注入队列（is_idle 死锁发生在这条路上）
```

**⇒ provision 排在 `is_delivery` / `buffer_if_busy` 之前，不进注入队列。**

**⇒ 即使一个会话 `is_idle` 死锁了（队列永远不 drain、一条消息都注入不进去），hub 仍能通过 provision 给它推新代码并重启 —— provision 走的是 recv_loop 的前置分支，绕开了死锁的路。**

**两个推论**：
- **provision 本身就是死锁会话的「远程解锁手段」** —— 不用 ssh、不用手动 kill。
- **`is_idle` 的修复可以远程推送给正在死锁的会话本身。**

---

## 4. 升级战略（分两段）

| 阶段 | 手段 | 为什么 |
|---|---|---|
| **`0.1.2.5` 首轮全网升级** | **最后一次手动**（本机 `cp`+重启 / ssh / 飞书通知装包） | 旧 helper 的 provision 白名单还是坏的（默认 `ts.wegoab.com`，连不上真 hub），**没法用 provision 自升** |
| **首轮之后** | **provision `update_self` 一键远程推送** | 白名单已修好；`is_idle` 修复、第 2 期、第 3 期、任何客户端变更，都不用再碰机器 |

**⇒ 首轮升级是「解锁自动升级能力」的钥匙。** 因此「通知符磊/谭平/Mac 装包」「找 secops71 owner」不是杂事，是**关键路径**——它们不完成，自动升级永远差最后一公里。

---

## 5. 🔴 部署验收项（`0.1.2.5` 上线时必做）

> **`update_self` 端到端实测 —— 不接受「测试绿」代替真机。**
> （provision 通道已静默损坏 3 天才被发现，教训：测试过 ≠ 生产通。）

**验收步骤**（在一个受控 helper 上，非生产关键会话）：
1. 打一个**版本号更高**的测试包（如 `0.1.2.6-canary`），放到 `dingwei.wegoab.com` 的 provision 下载目录，记 sha256。
2. hub 侧（admin）向该 helper 下发 `update_self`（url=测试包，sha256，version=`0.1.2.6-canary`）。
3. **验证**：
   - [ ] helper 下载成功（白名单放行真 hub —— 这一条同时验证 provision 白名单修复真的生效）
   - [ ] sha256 校验通过
   - [ ] 代码被替换、helper 重启、**新版本上线**（在线清单显示新版本号）
   - [ ] provision_ack 回到 hub（结果可见，非静默）
4. **回滚验证**：下发一个 **sha256 故意错误**的包 → 确认 helper **拒绝并回执**、**未替换代码**、**旧版本仍在线**。
5. **🔴 死锁场景验证**（本设计的关键卖点）：
   - 制造一个 `is_idle` 死锁的会话（用 TUI 类 CLI，或 mock `is_idle` 恒 False）
   - 向它下发 `update_self`
   - [ ] **确认 provision 仍被处理**（不受死锁阻塞）、helper 重启、死锁解除
   - 这验证了「provision 能远程救死锁会话」这条核心能力。

**任一项失败 → 自动升级能力不成立 → 记为部署阻塞，回到手动升级。**

---

## 6. 严禁（安全红线）

- ❌ **不得**新增「下发任意脚本 / 命令并执行」的 provision 动作。
- ❌ **不得**在 `update_self` 解包后执行制品内的任何脚本（`install.sh`、`__main__` 等）。
- ❌ **不得**放宽来源校验、sha256 校验、host 白名单任意一道。
- 任何触碰以上三条的改动，等于把「安全的代码分发」变成「全网 RCE 后门」——**必须走安全评审**。
