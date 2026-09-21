# 发布单草稿 — `0.2.1.50` 补齐修复 ＋ B1-a Agent 编组

目标版本: 待组长落号

    仓库       git@git.code.tencent.com:wego-ai/dingwei.git
    候选引用   refs/heads/coder56solzzc/b1-a-agent-groups
    当前候选锚 099277b6925fa4bea80e293cb3f327a5514455b8
    生产基线   bcd5f3376215f35902a1ef159e5d7827ed6bfb0a (`0.2.1.50`)
    业主确认   待确认

> 本件是预备草稿。目标版本、最终发版锚及业主确认尚未落下；不得据本件执行 merge、order 或 deploy，也不得在当前状态运行出单闸。

## 1. 目标

同批补齐 `0.2.1.50` 已确认但上线后未实际达成的 spec 管理路径，并交付 B1-a：

- spec 创建/保存失败原因对用户可见；management 所有者能编辑和提交自己的 spec。
- spec 读入口与写入口分开；关闭编辑后仍可读，关闭可由所有者重开且不得绕过 task/proposal revision 锁。
- console 左栏可在“编组-Agent 树”与“纯 Agent 列表”之间切换；可建删组、加入移出自己的 Agent、关闭添加。
- 五个未接线平台动作按钮退场，但服务端 `read/submit/approve/delete` 权限动词保持。

## 2. 问题

`0.2.1.50` 上线后现场暴露：必填项未标识、错误正文被吞、management 业主看不到 Edit/Submit、关闭编辑只约束能力链接却不约束所有者、只读 spec 无页面入口。

B1-a 之前只有旧 admin 最简编组入口；console 左栏没有用户侧建删组、成员管理或两形态切换。旧 admin 仍并存至 `0.2.2.0`，会持续产出 `creator_principal=NULL` 的组。

## 3. 本批改动与明确边界

同批提交链：

    d0fd729c  spec 必填提示与 create/save 服务端错误正文
    b73b5cee  management 所有者写权与 specAction 错误可见
    38c912f0  View 只读入口与写入口分离
    4453caa2  editing_closed 双路一致、仅所有者可重开、proposal 锁不被绕过
    2b582a93..8f06460f  B1-a 本体、历史组兼容、删空后 owner 隔离、孤儿兜底边界
    bdbfd402  reserved item map 机械拆行
    099277b6  交付边界与跨门副作用留痕

明确不做：B1-c 跨业主通知；B1-b admin tab 化；旧 admin 确认框；spec 的真实 tasks adapter。

6 字符短名是独立全局唯一显示标识，不截取组码，避免泄露完整 32 字符加入凭据的任何位。这是有意偏离“组码＋短名”可能被理解为前缀截断的字面实现。

## 4. 部署操作步骤

    1. 组长先落目标版本、业主确认与最终发版锚；核生产基线是最终锚祖先。
    2. 在干净取件目录运行 `build.sh`，按最终目标版本构建 workpulse。
    3. 备份现网 workpulse；替换 `/app/bin/workpulse`；supervisor 重启 `program:workpulse`。
    4. 启动时执行新增迁移 `0041_console_agent_groups.sql`。
    5. 核制品自报版本、`/healthz=200`、`/console=401`。
    6. 验收第 8 节后回流 preprod；未回流不得关单。

## 5. 审批需要什么

    生产基线 `bcd5f337` 是最终锚祖先                         待最终装配核
    目标版本单调高于 `0.2.1.50`                             待落号后核
    同批提交链完整且无范围外文件                            待最终 diff 核
    新迁移仅新增字段、唯一索引与 agent_group_state 单例表     待 CR
    业主确认                                                  待确认

🔴 旧 admin `POST /admin/agent-groups` 调用 `UpsertAgentGroup` 同样会把 `ever_activated` 置 1。任一入口创建第一个组，都会把全局路由从 pre-G 全通切到按组/owner 隔离，而旧 admin 页面没有提示。本批只明确留痕，不擅自增加确认框。

## 6. 授权什么

业主确认格当前为：待确认。

未取得确认且未由组长落入计划条目前，不授权 merge、order 或 deploy。生产动作仍由组长出单、prodops 执行。

## 7. 如何测试验证

发版前须在最终发版锚重新取得：

    go build ./...
    go vet ./...
    go test ./... -count=1
    python3 tools/run-browser-regressions.py

浏览器回执必须满足：`commit_sha == 最终发版锚`、`overrides=none`、SUITES 全集、`aggregate_rc=0`。父锚回执不得冒充最终锚回执。

迁移后只读核：`agent_group_state` 恰好一行；现网当前 `agent_group=0` 时 `ever_activated=0`；创建首组后为 1，删空后仍为 1。

## 8. 验收标准

    1. 四个 spec 必填项分别缺失时精确提示；服务端 create/save/action 错误正文可见。
    2. management 业主能编辑、提交自己的 spec；不能写别人的 spec。
    3. 关闭编辑后所有者与能力链接均不能写；所有者重开后可写；task/proposal 锁仍生效。
    4. 只读 View 对关闭编辑及他人 spec 均可进入，但没有可调用的写入口。
    5. 建组后左栏树出现 6 字符短名；完整 32 字符组码可见且是唯一加入凭据。
    6. 他人用完整组码将自己的 Agent 加入后，创建者树中显示该 Agent 全名；踢出后消失。
    7. 关闭添加后非创建者加入被拒，创建者仍可添加自己的 Agent。
    8. 删组自动解除全部成员关系；删光后跨 owner 路由仍拒绝；从未配置的迁移态保持 pre-G 行为。
    9. 五个未接线按钮为 0；spec 建/改/关闭/重开/开新版与其它 console 功能不受影响。
    10. 编组树与纯 Agent 列表互斥切换；未分组 Agent 只在纯列表形态出现。

## 9. 通过后谁该知晓

业主、coordinator、prodops、coder56solzzc、qwen38maxkhovvob4。

需逐字说明：现网取证时 `agent_group=0`，所以没有实际历史组被迁移；历史/孤儿兼容结论来自永久夹具，不冒充现网命中。旧 admin 到 `0.2.2.0` 前仍可能持续产生 `creator_principal=NULL` 的组，该兼容路径不是死代码。

## 10. 结束与回滚

回流 postflight 通过后关单；未回流不得关单。

代码回滚：退回 `0.2.1.50` 制品与对应运行配置。

迁移 `0041` 为追加式，代码回滚不删除新增列、索引或 `agent_group_state` 表。若业主明确授权回退到 pre-G 路由，须先确认 `agent_group` 为空，再由 prodops 执行：

```sql
UPDATE agent_group_state SET ever_activated=0 WHERE singleton=1;
```

该 SQL 是运维动作、不是产品入口，会放大跨 owner 可达范围；无业主授权不得执行。
