# 会话交接 · DingWei 全链 · 2026-08-25（本文件覆盖旧版）

> 🔴 **本文件不含任何会过期的数值。** 版本 / 锚 / 行字节 / sha256 一律去仓内取。
> ⚠️ 本轮教训（犯了三次）：**抄进文档的状态值，抄进去那一刻就开始过期。**

## 一、取件

```
仓      git@git.code.tencent.com:wego-ai/dingwei.git
分支    design/addendum-decisions
本地    /tmp/dw   （若不存在：git clone 后 checkout 该分支）
状态源  docs/DELIVERY-STATUS.md   ← 各件版本/末次改动锚/行字节/sha256，机器生成、随提交自动刷新
```

**接手第一件事**：读 `docs/DELIVERY-STATUS.md`，不要读本文件里的任何数字（本文件没有）。

## 二、当前处于哪一步（唯一要记的一句）

**SpecDoc 已【生效】，SP0–SP8 已解锁；下一步是动代码，不再是文档。**

| 件 | 门状态 |
|---|---|
| SpecDoc | 🔴 **生效** —— §0.4 两项生效前置均闭（Spec Kit 结构 ✅ · checklist 三域产出并执行、重走轮全 PASS ✅）|
| 架构设计 | 架构评审 ✅ PASS；**用例评审 / 交付验证未过** ⇒ **可作设计依据，不可作交付验收依据** |
| 原型 | 三域收口 |

⚠️ **「生效」不等于「全项闭合」**：§0.4 第 3 项（附录 E 探测手段）仍判「部分闭合」，见 E.5。

## 三、下一件技术工作：SP7a

**把仓储包外的直接库访问收编进仓储包。**
判据恒为 0；**口径与实数均以 `scripts/repo-boundary-gate.sh` 为单一来源**，本文件不复述数字与 API 清单
（复述一份就多一套口径 —— 本轮已在 `collect.sh` 标签、快照包作用域、`specs/README` 索引上栽过三次）。

> 🔴 **收编面由甲组【三条】判据共同定义，不是由 `sql.Open` 单一指标定义。**
> 2026-08-25 本席曾用单指标去推整个收编面，得出「fanout 只有 spool.go、交接件过列」——**是错的**，
> 差点把一份正确的表改错。用哪条判据看，收编面就不一样；报「与记载不符」前先问自己证据是否覆盖全部三条。

| 序 | 对象 | 要点 |
|---|---|---|
| 1 | `internal/datad/pgstore.go` | PG 是跨租户数据主场，先收 |
| 2 | `internal/fanout/{spool,inspool,admin}.go` | 出站含租户数据，泄漏面直接。**本行三名按【建表】与【裸 SQL】入列；其中仅 `spool.go` 持库句柄** —— `inspool.go` 与 `admin.go` 虽不持句柄，但各有建表点与裸 SQL，**三名均在收编面内**，勿据「谁持句柄」缩小它。<br>实数现取：`grep -c "CREATE TABLE" internal/fanout/*.go` 与 `grep -cE "\.(Exec\|Query\|QueryRow)" internal/fanout/*.go` |
| 3 | `internal/mcs/board` | 🔴 **须待 conformance suite**（§4.5：无它之前不得以「重写」为由丢弃任一既有并发语义）|
| 4 | `internal/store` | 已是仓储形态，补 `TenantCtx` 与迁移 runner |

**验收判据两组**（架构设计 A§12）：
- 甲组·收口：仓储包外**直接库访问**恒零（口径见闸）· 未被仓储包引用的表恒零 · 漏传 `TenantCtx` **编译失败**
- 乙组·隔离：跨租户读**四项同时**验（PEP 在资源解析前拒且 query spy=0 · 语义 deny 禁成功空集 · 存在/不存在不可区分 · 最小化拒绝审计）
  🔴 **①②须分开验**，否则漏掉「先查了再拒」

**`board.task` → `board_task` 改名**：名已裁（SpecDoc §5.6a 权威定义），**改名随 board 收编做**；
过渡纪律已生效：**新代码不得再新增对 `board.task` 的引用**。

## 四、闸（提交时自动跑，勿绕）

```
scripts/speclint-gate.sh        提交闸：⓪钩子自检 ①正文 lint ②语料 ③变异体 ④原型版本 bump ⑤状态源同步 ⑥架构引用核验(含四反测)
scripts/arch-refcheck.sh        架构引用核验（A§本件 / §SpecDoc 分别解析）；--selftest 跑四项反测
docs/tools/speclint.py          SpecDoc lint（16 条规则）；--selftest 语料；--mutate 变异体
specs/tools/analyze.sh          /speckit.analyze 跨制品一致性
specs/tools/export-bundle.sh    导出锚定快照包（清单由 git ls-tree 出，不手写）
specs/evidence/collect.sh       生成证据包（每条断言附产出命令）
scripts/delivery-check.sh       投递目录自检（滚动目录 / 锚定快照包 两形态）
```

🔴 **钩子是软链**：`make speclint-hook`。若 `.git/hooks/pre-commit` 是拷贝，闸会拒绝提交并提示。

## 五、协作纪律（本轮血换来的）

| # | 规矩 |
|---|---|
| 1 | 报锚**现取** `git log -1 --format=%H -- <当前路径>`，不用记忆里的值（改名后锚变而 sha256 不变）|
| 2 | 消息带【发现 → 锚】累计表 —— coordinator **无仓访问**，消息是他唯一通道 |
| 3 | 报修写「修在锚 Y，前锚 X」；收信先实算比对再动手 |
| 4 | 投递名带版本号；仓内名**不带**版本号；被取代件加 `SUPERSEDED-` 前缀而非删除 |
| 5 | checklist 是 reviewer-owned —— **执笔方不得代写、不得代勾** |
| 6 | 门方判 FAIL：**先修再提异议**，不拿异议挡修 |

## 六、待办

| # | 事项 | 谁 |
|---|---|---|
| 1 | SP7a 收编（见三） | 本席 |
| 2 | ⏭️ 转交合同 12 项的实际验收（架构设计 A§21，每项五列齐）| 各 SP |
| 3 | C.2 余下：知识库首批项目 · 两次收敛会 · 分发端点生产可达性 · 灰度通道授权 | 业主 / ops |
| 4 | 附录 E 第 3 项「部分闭合」的完全闭合 | 见 E.5 |

## 七、方法层教训（长期笔记，此处只列索引）

| 笔记 | 一句话 |
|---|---|
| `lint-rule-must-have-teeth` | **七种「闸没起作用」的形态，全部让人看到「通过」** —— 看到 ✓ 先问它测了没有 |
| `three-self-made-defect-classes` | 六类自造缺陷：合并未对撞 / 按名字判 / 改规则未同步引用点 / 刚立的规矩在下一段自破 / 安全属性互相冒充 / 依赖≠路径 |
| `dingwei-review-handoff-discipline` | 交接坐标 → 锚标注 → 拉取式状态源 → 消息自足；跨文档引用须带来源类型 |
| `verifiable-tiebreaker` | 择一时判据本身须可复核；「谁改动小」不可复核 |
| `hash-must-be-fully-computed` | 精度不得超过实取精度；改名后锚失效而 sha256 不变 |
| `escalation-restraint` | 工程判断自定，只有改权限边界或业主承诺才上呈 |

🔴 **本轮复发最多的一条**：**改一处漏一处**（≥6 次，含属性头块三次）。
接手后凡改「同一内容存在两处」的东西，**先搜全量引用点再动手**。
