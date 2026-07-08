# DingWei 平台总控路由架构规格 v0.1

> 目标：给 DingWei 一个**平台级总控**——规则优先、LLM 兜底、任务队列保交付、可水平扩并发。
> 核心原则：**L1 规则过滤挡掉 90% 流量（零 LLM、高并发）；L2 LLM 只处理判不了的难题（有界并发）；一切走队列，保证每个请求都有响应。**
> 定位：总控是**无状态可扩的平台组件**，不是某个业务会话（manager 不背此责）。

---

## 一、三层架构

```
入站(飞书私聊/群 或 会话主动发起)
        │
        ▼
┌─────────────────────────────────────────────┐
│ L0 任务队列  control_task 落库 + 立即 ack(#id)  │  ← 保交付的地基
└─────────────────────────────────────────────┘
        │  取任务
        ▼
┌─────────────────────────────────────────────┐
│ L1 规则过滤器(同步、廉价、无 LLM)              │
│  命中确定性规则 → 直接处理/投递 → 出队 done     │
│  判不了 → 标 llm_pending，交 L2               │
└─────────────────────────────────────────────┘
        │  llm_pending
        ▼
┌─────────────────────────────────────────────┐
│ L2 LLM 分诊/编排(有界 worker 池,N 并发)       │
│  分诊→派发 / 分解→多子任务 / 聚合 / 消歧 / 拒绝  │
│  产出目标+指令 → 经 Hub 投递 → 等回执 → done   │
└─────────────────────────────────────────────┘
```

- **L1** 是主路径：现有的前缀/地址/owner 路由都在这层，已经是规则式。
- **L2** 只在 L1 判不了时启动，成本可控。
- **L0** 让"每个请求都能得到响应"成立：入队即受理、持久、超时/失败有兜底。

---

## 二、任务队列 schema（SQLite，`control_task`）

```sql
CREATE TABLE control_task (
  id             TEXT PRIMARY KEY,      -- request_id (uuid hex)
  parent_id      TEXT,                  -- 子任务指向父任务(分解场景)，顶层为 NULL
  created_at     TEXT NOT NULL,         -- ISO8601 UTC
  updated_at     TEXT NOT NULL,
  source         TEXT NOT NULL,         -- feishu | session
  source_addr    TEXT NOT NULL,         -- 回复地址: <open_id>#<key>#<bot> 或 <会话>#<key>
  owner_key      TEXT NOT NULL,         -- 解析出的账号(用于 owner 范围路由/鉴权)
  bot_channel_id TEXT,                  -- 飞书回投用
  raw_input      TEXT NOT NULL,         -- 原始正文
  intent         TEXT,                  -- route.session|command.*|nl_dispatch|decompose|aggregate|clarify|unknown
  layer          TEXT,                  -- L1 | L2 (最终在哪层解出)
  target         TEXT,                  -- 解析出的目标(JSON 数组: [{session,key,instruction}])
  result         TEXT,                  -- 最终回复/结果
  status         TEXT NOT NULL,         -- 见状态机
  priority       INTEGER NOT NULL DEFAULT 0,   -- 越大越急
  attempts       INTEGER NOT NULL DEFAULT 0,
  max_attempts   INTEGER NOT NULL DEFAULT 3,
  error          TEXT,
  lease_owner    TEXT,                  -- 持有该任务的 worker id(并发租约)
  lease_until    TEXT,                  -- 租约到期(worker 崩溃后可被别人抢回)
  expire_at      TEXT                   -- 整体超时点(超过=expired)
);
CREATE INDEX idx_ct_status_prio ON control_task(status, priority DESC, created_at);
CREATE INDEX idx_ct_parent ON control_task(parent_id);
```

### 状态机
```
queued ──L1──▶ done              (命令/直接路由即完成)
   │           
   │    ┌──────────────▶ dispatched ──▶ awaiting_result ──▶ done
   ▼    │                                    │
llm_pending ─(L2 lease)─▶ (dispatch/decompose)                
   │                          │(decompose)                     
   │                          └──▶ 生成子任务(parent_id=本id)，本任务转 awaiting_children
   │                                          │子全 done                
   └─(失败/无 worker)                          └──▶ aggregate(L2) ──▶ done
   
任意态 ─(超 expire_at)─▶ expired ──▶ 给 source 明确超时回复
任意态 ─(attempts≥max)─▶ failed ──▶ 给 source 明确失败回复
```

---

## 三、L1 决策表

> 有序匹配，命中即停。`出队` = 该层直接产生结果/投递，不进 L2。

| 序 | 匹配方式 | 模式 | intent | 动作 | 出队? |
|---|---|---|---|---|---|
| 1 | 前缀 | `#unlock <码>` / `#lock` | command.unlock | grantTerminalInput/revoke | ✅ 即时 |
| 2 | 前缀 | `#在线` / `#roster` | command.roster | 回在线清单 | ✅ |
| 3 | 前缀 | `#申请 <会话名>` | command.apply_key | 发 key 流程(可带审批) | ✅ |
| 4 | 前缀 | `#mirror on/off` | command.mirror | 开关镜像 | ✅ |
| 5 | 正则 | `^#<会话名>\s+正文` | route.session | routeToSession(同 owner 解析) | ✅ 投递 |
| 6 | 正则 | `^@<成员>#<会话名>\s+正文` | route.cross | routeToSession(跨会话) | ✅ 投递 |
| 7 | 默认 | 私聊无前缀 且 该账号仅 1 个在线会话 | route.default | 投递到唯一会话 | ✅ |
| 8 | 默认 | 私聊无前缀 且 多会话在线(判不了发谁) | nl_dispatch | 转 L2 分诊 | ❌→L2 |
| 9 | 默认 | 含"让团队/大家/所有人 …"等多目标语义 | decompose | 转 L2 分解 | ❌→L2 |
| 10 | 兜底 | 都不匹配 | unknown | 转 L2 消歧(或反问) | ❌→L2 |

> 规则表**数据化**（存表或配置），可热改；新增规则不改代码。现有散在 Hub 各处的路由，第一步就是收拢成这张表。

---

## 四、L2 LLM 分诊接口

**输入（喂给分诊 LLM 的上下文）**
```json
{
  "request_id": "…",
  "raw_input": "让团队复测登录全流程并汇总",
  "source": "feishu",
  "owner_key": "zzc",
  "online_sessions": [
    {"session":"zzc-developer","tool":"CODEX","model":"gpt-5.5","role":"dev","busy":false},
    {"session":"zzc-tester","tool":"CLAUDE-DEEPSEEK","model":"deepseek-v4-pro","role":"qa","busy":false},
    {"session":"zzc-manager","tool":"CLAUDE","model":"opus","role":"coordinator","busy":true}
  ],
  "recent_context": "…可选，最近几轮…"
}
```

**输出（LLM 必须返回的结构化 JSON，工具/schema 强约束）**
```json
{
  "intent": "dispatch | decompose | aggregate | clarify | reject",
  "reply": "给发起人的话(clarify/aggregate/reject 时用)",
  "targets": [
    {"session": "zzc-tester", "instruction": "复测登录全流程，回执用 #发起会话名 开头"}
  ],
  "subtasks": [
    {"session": "zzc-developer", "instruction": "自查登录接口最近改动"},
    {"session": "zzc-tester", "instruction": "端到端复测登录并记录异常"}
  ],
  "confidence": 0.0
}
```

**分诊语义**
- `dispatch`：单目标，直接派发（targets 单元素）;
- `decompose`：多目标，生成 subtasks（每个落成子 control_task，parent_id=本任务）;
- `aggregate`：子任务全回执后，L2 再调一次把多份回执综合成 reply;
- `clarify`：信息不足，回问发起人;
- `reject`：非法/越权/无可用会话，礼貌拒绝。
- `confidence < 阈值` → 降级为 clarify（宁可反问，不乱派）。

**L2 worker 池**：从队列 `WHERE status='llm_pending' ORDER BY priority DESC, created_at` 抢租约（lease_owner/lease_until），并发 N（可配，先 4）。LLM 是瓶颈，队列削峰。

---

## 五、保交付 / 并发 / 可观测

- **入队即 ack**：`已受理 #<id>`，发起方不悬空;
- **持久化**：落 SQLite，平台重启不丢；租约到期的在途任务可被重抢（崩溃恢复）;
- **超时兜底**：每任务有 `expire_at`（如 5 分钟），reaper 扫到超时→标 expired→回明确超时;
- **重试**：瞬时失败 attempts+1 重排，`≥max_attempts`→failed→回明确失败;
- **幂等**：request_id 去重，重复投递不重复执行;
- **背压**：队列深度暴涨时，L1 仍即时（不受影响），L2 排队；可加扩 worker 或拒绝低优先级;
- **可观测**：队列深度、各状态计数、L2 处理时长 P50/P95、失败率、超时率 → 指标/日志。

---

## 六、与现有 DingWei 集成

- 现有 Hub 的入站处理是"直接路由"。改成：**入站先入 `control_task`**，`RouteEnvelope`/命令分发下沉为 **L1 规则执行体**;
- L1 命中→照旧即时处理（几乎零改动、零延迟）;判不了→置 `llm_pending`;
- L2 是**新增的 worker 池**（Go goroutine 池 + 队列租约），调用一个分诊 LLM（可用便宜快模型，如 haiku/deepseek）;
- 投递仍走现有 `routeToSession`（含刚做的 owner 跨 key）;回执/聚合复用 send.py 那套地址体系。

---

## 七、分期实施

1. **P1 队列骨架 + L1 数据化**：建 `control_task`、入队/ack/持久/reaper/重试；把现有路由收拢成 L1 决策表；**此期不接 LLM**，纯规则也能保交付+可观测。
2. **P2 L2 最小分诊**：接一个分诊 LLM，只做 `nl_dispatch`（无目标自然语言→选一个 agent）+ `clarify`；worker 池 N=4。
3. **P3 编排增强**：`decompose` 多子任务 + `aggregate` 聚合；优先级调度。
4. **P4 扩展**：跨账号/多租户隔离、审批钩子、指标看板、动态扩 worker。

---

## 八、关键决策（2026-07-08 已拍板 ✅）
- **分诊 LLM = deepseek**（成本敏感、快）；
- **L2 并发 N = 4**；**任务超时 = 5min**；**重试上限 = 3**；
- **队列 = SQLite**（够用、与现状一致，不引独立队列）；
- **L1 决策表 = DB 表**（可热改，按建议）。
