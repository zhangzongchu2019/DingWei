# coder56solzzc 当前任务交接 · 2026-09-20

## 会话状态

- 当前会话可用。
- 本件仅保存恢复上下文；未开展额外排查或产品变更。

## 已收口

### ts-No.72 · D-2 preflight 接 stagegate

- 分支：`coder56solzzc/stagegate-preflight`
- 当前提交：`e64b2d755d76a2c7f65e5245f56baf358fbe95f0`
- `preflight` 已改为：断言格逐格执行并汇总；环境/参数不可判仍立即 `rc=3`。
- 拆分号母版不再写死，由计划件 `拆分号相应为` 的逐字授权反推。
- 实跑：`0.2.1.31 → 0.2.1.34 → 母版未确认`，stage 格红，整体 `rc=2`。
- 阳格：`0.2.1.7` 的 stage 格绿；其他旧闸红不影响该格判定。
- 删除 stage 接线变异：stage 格消失；两张真实单其余各 10 格逐格不变，`nonstage_diff31=0`、`nonstage_diff17=0`。
- 工作树恢复干净，变异未提交。

### ts-No.74 · console seed 只读定界

- `UpsertConsolePrincipal` 冲突目标仅为 `id`；身份三元组另有 UNIQUE，因此“换 id、同三元组”直接报 2067。
- 生成器把命令行第一个 `--principal-digest` 机械编号为 `owner-entry-a`，第二个为 `owner-entry-b`；subject 由各 digest 唯一解析的 open_id 决定。
- 新 seed 把 b 的 digest 排在第一位，故 b 的 subject 被贴到 a 的稳定 ID；不是 display_name 或数据库行序问题。
- `seedConsoleACL` 对 principals、grants、principal roles、platform roles、agents 均为逐条 upsert，无缺失删除/停用语义。
- `owner-entry-b` 缺席于 seed 不会删除其既有 21 条 grants。
- 计数 INFO 位于全部装载成功之后；principal 冲突时不可达，帮不到失败侧定位。
- 全程未触生产、未修改 seed、未重启。

## 待恢复

### `0.2.1.50` · speckit

- 原实现分支：`coder56solzzc/spec-management-02110`
- 当前锚：`b9bf8ba026da72757e110135b25e86470def9ab0`
- 业主已明确 `speckit 解冻 GO`，版本由 `0.2.1.10` 改为 `0.2.1.50`。
- 组长随后暂停实现，要求先完成 ts-No.74；目前尚未恢复实施。
- 编辑器选型已转交 qwen，并已定 EasyMDE；本席不再做选型。
- 恢复前应等组长重新给单，然后先核最新发布线/计划件，再按批准范围实施。
- 已知高风险：完整 `specify → plan → tasks → implement` 若要求真实任务落地，依赖尚未实现的 K adapter；不得把 proposal 冒充已接通任务流。

## 边界

- 不擅自部署或触碰生产。
- 不继续修改已收口的 ts-No.72，除非新事实触发明确解冻。
- 恢复 speckit 时，版本三处必须统一为 `0.2.1.50`，并以最新发布线为基线。
