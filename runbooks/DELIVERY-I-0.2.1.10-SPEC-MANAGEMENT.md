# 0.2.1.10 I: specKit 与需求管理界面交付件

## 授权与范围

- 开工依据:业主 2026-09-20 常设授权——符合北极星且不属于产品定义或大架构调整的事项可向前推进。
- stagegate 中逐条指标仍保持“未确认”;本实现没有改闸让自身通过。
- 本版只交付多-spec 运行时、console 管理页和经人审的结构化 task proposal。
- 本版不写 `ControlTask`、schedule、git `tasks/T-*.json` 或 `datad.Task`;不提供 K 的 assignee/reviewer/priority/status。该缺失是有意的 I/K 边界,不是漏字段。

## 实现

- 新增迁移 `0039_console_specs.sql`:spec、不可变 revision、双门 review、proposal 与事件留痕。
- 同一 principal 可并存多份 spec;普通 principal 对他人 spec 的 GET/PATCH 与不存在统一为 404。
- revision 以 CAS 追加;认领后产生新 revision 会清空当前 `claimed_by`,保留历史 claim 事件并回到 `needs_improvement`。
- hard gate 与 human review 必须在同一 revision 上分别 PASS;任一 FAIL 必须带非空结构化 reason code。
- proposal 以 `(spec_id,revision)` 幂等;历史保留,ready 视图只暴露当前 revision 且状态为 claimable/in_progress 的一份。
- C2 平台壳新增 Specs builtin,以独立页 `_blank` 打开;真实页面可新建、同时列出、分别编辑多份 spec,并提供 submit/review/claim/archive 动作。

## 可证伪证据

- 列表变异为只取第一项:浏览器用例因看不到 Spec B 转红,rc=1;恢复后 rc=0。
- 删除 hard-gate 条件并允许 human-only:同 revision 双门测试在 `human-only review` 转红。
- 把 proposal 生成放宽为任意 PASS:测试检出 hard-only review 创建隐藏 proposal并转红。
- 在 spec handler 中加入 `EnqueueControlTask` 调用:真实 AST 边界闸唯一转红。
- 失败 review 的空数组、空字符串、空 reason 与非数组均被拒;带 reason code 后回到 `needs_improvement`。

## 回归

- `go build ./...`:rc=0。
- `go vet ./...`:rc=0。
- `go test ./... -count=1`:rc=0。
- 浏览器聚合入口五套均 rc=0,aggregate rc=0。该次本地回执明确记录 `NODE_PATH` 与 `PLAYWRIGHT_BROWSERS_PATH` override,因此只作实现回归证据,不得冒充 release-gate 所需的 `overrides=none` 发版回执。
- 新 console spec 浏览器套件:真实页面同时显示两份 spec,分别进入编辑并在重载后保留各自 revision。

## 明确未跑/未证明

- 未部署、未改生产、未执行数据库生产迁移。
- 未做业主页面实操验收。
- 未证明 task proposal 已被 K 消费;adapter 明确归 K。
- 未把 stagegate 的“未确认”改为“已确认”。
- 未生成可用于出单的 `overrides=none` 浏览器回执。
