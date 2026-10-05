# R-V2-SEAT-A1-JOBVIEW-20261005002805 · 生命周期 job 契约

代码锚：`coder56solzzc/v2-seat-items-a`（本文随实现 tip 一起更新）；版本保持 `0.2.1.100`。

## 1. 写接口与四态

`POST /api/console/hosted/agents/{agent_id}/{op}` 在完成认人、归属、参数与当前席位状态预检后，持久化 job，返回：

```json
{"job_id":"job_<hex>"}
```

HTTP 状态为 `202 Accepted`。`invalid_state`（含非托管席位）在入队前同步返回 `422`，不会产生必败 job。

生命周期 job 只有四态：

| stage | 含义 |
|---|---|
| `queued` | 已持久化，worker 尚未取得 |
| `running` | worker 已取得；restart／enable 还包括等待 Agent 重连阶段 |
| `completed` | 本操作的成功终态已经成立，响应同时带 `agent_id` |
| `failed` | 失败终态，`reason` 为机器码 |

创建 job 的既有 `queued/pulling/starting/online/failed` 契约不改；同一个 jobs 读端点先识别 lifecycle job，不存在时再读创建 job。

## 2. 五操作的成功终态

| op | completed 的判据 |
|---|---|
| `upgrade` | 节点升级成功，镜像引用已更新，席位回到 `online` |
| `restart` | 节点接受重启后仍为 `running`；专属 Agent 真正重连、席位置 `online` 后才 `completed` |
| `disable` | 凭据已暂停、容器已停止、席位置 `disabled` |
| `enable` | 节点接受启动后仍为 `running`；专属 Agent 真正重连、席位置 `online` 后才 `completed` |
| `destroy` | 备份完成、凭据吊销、容器与配额清理、账号回池、席位置 `destroyed` |

restart／enable 的五分钟 `starting` 超时由既有席位清扫器写 `error/start_timeout`，jobs 读面随即收敛为 `failed/start_timeout`，不会过早报 completed。

Hub 启动时会一次性收敛上个进程遗留的 `queued|running` lifecycle job，保证轮询不永挂：

- `restart`／`enable` 且席位已经 `online`：`completed`；
- 其余遗留 active job（包括终态无法可靠还原的 upgrade／disable／destroy）：`failed/internal_error`；
- 不重新执行可能已有部分副作用的生命周期命令；业主可在看到失败后按当前席位状态重新发起。

## 3. 失败机器码全集

`failed.reason` 只出以下机器码；Go/数据库/节点原始错误不进入页面：

- `invalid_state`
- `node_offline`
- `start_failed`
- `start_timeout`
- `upgrade_failed_rolled_back`
- `backup_failed`
- `params_invalid`
- `not_owner`
- `confirm_required`
- `internal_error`（无法归入上述已知码的兜底）

其中 Hub 重启后无法证明成功终态的遗留 job 也使用 `internal_error`，不增加第十一个机器码。

能在入队前确定的 `invalid_state`、`not_owner`、`confirm_required`、`params_invalid`、节点当前离线等，仍走同步错误；机器码全集也覆盖“预检后到 worker 执行前状态发生变化”的竞态。

## 4. 重复 job：取幂等复用

同一席位只允许一个 `queued|running` lifecycle job，由 PG 部分唯一索引兜并发：

- 同操作重复点击：返回已有 `job_id`，仍为 `202`，不重复执行；
- 不同操作撞上 active job：同步 `422 invalid_state`；
- 前一 job 到 `completed|failed` 后，才允许创建下一 job。

这避免 disable/restart、destroy/upgrade 等不同状态机同时改同一席位，也让浏览器重试安全。

## 5. DDL、幂等与回滚兼容

新增表与索引均使用 `IF NOT EXISTS`：

```sql
CREATE TABLE IF NOT EXISTS g4_lifecycle_job (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL REFERENCES g4_hosted_seat(agent_id),
  created_by TEXT NOT NULL,
  op TEXT NOT NULL CHECK (op IN ('upgrade','restart','disable','enable','destroy')),
  args JSONB NOT NULL DEFAULT '{}',
  stage TEXT NOT NULL DEFAULT 'queued'
    CHECK (stage IN ('queued','running','completed','failed')),
  reason TEXT NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS g4_lifecycle_job_agent_idx
  ON g4_lifecycle_job(agent_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS g4_lifecycle_job_one_active_idx
  ON g4_lifecycle_job(agent_id) WHERE stage IN ('queued','running');
```

- 幂等：`ApplySchema` 重复执行不改现有行；已有十表、没有 lifecycle-job 表的旧形状库可直接启动并补表，席位行保持不变，已有真 PG 格覆盖。
- 旧 Hub 回滚：旧二进制不查询该新表；保留表不影响旧读写。回滚无需删表，避免丢失审计中的 job 结果。
- 与 B2：A1 DDL 与 B2 的模型字段变更没有语义依赖，可在同一部署窗口由同一次幂等 `ApplySchema` 执行，但各自必须有独立旧形状升级格；不要求互相等待，也不把两项塞进同一 SQL 原子迁移。

## 6. 部署单必须写明

1. 换 Hub 前备份 PG；启动新 Hub 时执行幂等 hosted schema apply。
2. 核 `g4_lifecycle_job` 三个索引存在，旧席位数与升级前一致。
3. 回滚只换回旧 Hub；新表保留，不执行 destructive DDL。
4. 新旧 Hub 不应长时间并跑；旧 Hub 不产生 lifecycle job，切回后生命周期接口恢复旧同步语义。
