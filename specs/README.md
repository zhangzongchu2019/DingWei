# specs/ —— Spec Kit 工程结构

落成 SpecDoc **§0.4 第 1 项生效前置**（Spec Kit 形态承载）。

## 结构

> 🔴 **本表的路径列由闸逐行核对**（`scripts/specs-index-check.sh`，提交时跑）：
> 表里列的路径必须在仓内真实存在，仓内 `specs/` 下的件也必须在表里出现，**两边都不许有余数**。
> 立此判据的直接原因：本表原为手写树形，自称是 `specs/` 的结构，实际 15 件只列了 7 件 ——
> `specs/evidence/` 三件与 `specs/tools/export-bundle.sh` 早已漏列，`specs/gates/` 两件与
> `export-exclude.txt` 是新增后未同步。**「改了没同步引用点」在同一天同一目录里的第三次。**
> ⇒ 与 `export-bundle.sh` 那条同源：**凡说「全部」，清单就不能只靠人手维护。**

| 仓内路径 | 说明 |
|---|---|
| `specs/README.md` | 本文件：`specs/` 的结构说明与取舍记录 |
| `specs/constitution.md` | 治理原则的【唯一】载体（§4.1） |
| `specs/platform/spec.md` | 装载点 —— 指向 `docs/` 的正文，不复制、不搬动 |
| `specs/platform-architecture/spec.md` | 新版平台架构设计的施工与验收合同；冻结分域、接口与技术架构三类输出，旧架构件只作历史参考 |
| `specs/platform-architecture/check.sh` | 新版架构件的非门控形状检查：反查包/进程、原型入口与接口 ID；明确不替代语义评审 |
| `specs/platform-architecture/cutover-check.sh` | 架构唯一事实源切换核验：机器派生预期/实检/未知集合，未知非空即失败 |
| `specs/sp0-1/spec.md` | SP0 / SP1 两包分开同时做的协调合同；冻结共同输入与独立开工门 |
| `specs/sp0-1/contract-check.sh` | SP0、SP1 独立合同/目标路径、交付 ID 与冻结输入核验 |
| `specs/sp0/spec.md` | SP0 总则与定义层独立设计合同（§1–§4、附录 A） |
| `specs/sp0/contract-check.sh` | SP0 独立范围、六面、交付 ID 与目标路径核验 |
| `specs/sp0/check.sh` | SP0 正文章节、附录 E、原型入口及交付 ID 的机器派生覆盖核验 |
| `specs/sp1/spec.md` | SP1 身份与租户地基独立设计合同（四项业主点名范围） |
| `specs/sp1/contract-check.sh` | SP1 独立范围、六面、交付 ID、四项点名范围与目标路径核验 |
| `specs/sp1/check.sh` | SP1 正文章节、附录 E、原型入口、交付 ID 与合同红线覆盖核验 |
| `specs/sp2/spec.md` | SP2 执行层与门禁设计合同（§5–§9，含 WRK schema 的 SP7b 责任） |
| `specs/sp2/contract-check.sh` | SP2 六面、交付/验收 ID、WRK 接口与冻结输入核验 |
| `specs/sp2/check.sh` | SP2 六类覆盖集合机器派生、结构化落点与正交变异反测；目标件未建时 exit 2 |
| `specs/sp3/spec.md` | SP3 上下文与知识库设计合同（§10–§13，含 KNO schema 的 SP7b 责任） |
| `specs/sp3/contract-check.sh` | SP3 六面、交付/验收 ID、KNO 接口、新轮换协议与冻结输入核验 |
| `specs/sp3/check.sh` | SP3 四类覆盖集合机器派生、结构化落点与正交变异反测；目标件未建时 exit 2 |
| `specs/sp3/REVIEW-HISTORY.md` | SP3 旧轮次只归档不转效力、新 N1–N8 轮换与 recused 窄票边界 |
| `specs/sp3/review-aggregate.py` | SP3 单主票与 recused 面恰一张无涉窄票的聚合门及 N1–N8 夹具 |
| `specs/sp3/review-aggregate.sh` | SP3 票据聚合门入口 |
| `specs/sp8/spec.md` | SP8 授权与平台治理设计合同（§23、§24，含 GOV 治理面 schema 的 SP7b 责任）|
| `specs/sp8/contract-check.sh` | SP8 六面、交付/验收 ID、GOV 接口、同域差集与组合冻结输入核验 |
| `specs/sp8/check.sh` | SP8 四类覆盖集合机器派生（含 GOV 同域差集）、结构化落点与正交变异反测 |
| `specs/sp8/EVIDENCE-blocker1.md` | SP8 合同阻断的拉取式闭合证据 |
| `specs/sp8/EVIDENCE-body-rounds.md` | SP8 正文各轮 findings 与闭合锨对照卷 |
| `specs/sp4/spec.md` | SP4 skill/MCP 体系设计合同（§14–§16，含 CAP schema 的 SP7b 责任）|
| `specs/sp4/contract-check.sh` | SP4 六面、交付/验收 ID、CAP 接口与事件、前置处置与组合冻结输入核验 |
| `specs/sp4/check.sh` | SP4 五类覆盖集合机器派生、结构化落点与正交变异反测；目标件未建时 exit 2 |
| `specs/sp4/xref-check.sh` | SP4 跨件数量引用、冻结锨和区间派生器的机器门 |
| `specs/sp4/REVIEW-REGISTRATION-v1.3.md` | SP4 v1.3 合同 3/3 PASS 的历史登记卷 |
| `specs/sp4/REVIEW-REGISTRATION-v1.8.md` | SP4 v1.8 合同 3/3 PASS 的冻结登记卷；只记被评件指纹，不内嵌自身哈希 |
| `specs/sp6/spec.md` | SP6 交付合同；按 v0.2 域轴拆出 SP6-AGT 与 SP6-MSG 两个包 |
| `specs/sp6/check.sh` | SP6 正文覆盖、AGT/MSG 接口全集与非空预期集合核验 |
| `specs/sp6/check.py` | SP6 checker 实现 |
| `specs/sp7/spec.md` | SP7-PLT 合同；覆盖 SP7a 机制基座并钉死 SP7b 领域迁移边界 |
| `specs/sp7/check.sh` | SP7 正文覆盖、PLT 接口全集与非空预期集合核验 |
| `specs/sp7/check.py` | SP7 checker 实现 |
| `specs/sp6-sp7/DECISIONS.md` | SP6/SP7 合同依赖的仓外 DingWei 业主裁定索引与验证边界 |
| `specs/tools/domain-package-check.py` | SP6/SP7 共用域包 checker：冻结输入、按列派生、结构映射、三态与常驻变异反测 |
| `specs/v0.2/spec.md` | v0.2 域主轴＋SP 交付封套的统一基准合同 |
| `specs/v0.2/check.sh` | v0.2 可达性、域等价性、错配登记、七条义务与 RV 完整性总门 |
| `specs/v0.2/check.py` | v0.2 总门实现 |
| `specs/v0.2/domain-baseline.tsv` | 存量 SP 章节范围与 primary domain 的机器基线 |
| `specs/v0.2/baseline-manifest.tsv` | v0.2 当前树权威件的路径、sha256 与来源锨棘轮 |
| `specs/v0.2/domain-mismatches.tsv` | §17 域 owner 与存量 SP 声明域的逐行错配登记 |
| `specs/v0.2/future-domain-packages.tsv` | SP5-COL、SP6-AGT、SP6-MSG、SP7-PLT 的域派生包与设计收口状态清单 |
| `specs/v0.2/obligations.tsv` | v0.2 整合的七条跨件义务台账 |
| `specs/platform/checklist.md` | 结构域（sol 主笔，26 项） |
| `specs/platform/checklist.security-reviewer.md` | 安全域（terra，18 项） |
| `specs/platform/checklist.crossartifact-reviewer.md` | 跨制品域（kimi，12 项） |
| `specs/platform/checklist.INDEX.md` | 作者维护的登记，不判定 |
| `specs/evidence/CROSSARTIFACT-REVIEW-v0.2.md` | `ccccd4f` 统一树上的 CAC-1～12 复跑证据；记录 9 PASS / 3 FAIL 及其后续 SP1/CAC-6 收口入口 |
| `specs/templates/spec-draft-selfcheck.md` | **产品方**起草 spec 时的自查件 —— 属性头块（§5.2 三方分填）＋ 内容五面（§4.7.1）＋ 内部自相矛盾自查。🔴 **不判门**（硬门 §4.6.2 / 人审 §4.6.4），**通篇不用 `[ ]` 形态** —— 起草方自备带勾选框的表自己勾，形式上与自签门 PASS 无法区分（§4.1 · §7.2）。⚠️ 本件是 DingWei 侧附加件，**不是 Spec Kit 模板**（后者 immutable，§4.2 要求 2 · §16.5，连提案都不能提）|
| `specs/tools/analyze.sh` | `/speckit.analyze` —— 跨制品一致性检查 |
| `specs/tools/export-bundle.sh` | 导出锚定快照包；作用域由 `git ls-tree` 整棵导出，不手写 |
| `specs/tools/export-exclude.txt` | 快照包的显式排除清单，每条须带理由；缺理由或漂移即导出失败 |
| `specs/tools/handle-escape/main.go` | 裸库句柄逃逸检测器（Go AST）—— 判据二的权威 detector；覆盖换行签名 · import alias · 类型别名链 · 导出字段 · 匿名嵌入 · 接口方法 |
| `specs/gates/checklist-execution-exceptions.txt` | checklist 未落账的**登记例外** —— 🔴 登记 ≠ 修复；闸打印「登记债务」时**不是 PASS**；例外的新增与批准属 owner/门方评审面，执笔方不得自增自批 |
| `scripts/rv-integrity-gate.sh` | RV 完整性闸 —— 标注是 **reviewer-owned 的证据**，执笔方的清理/替换工具不得改写其正文；🔴 与「改一处漏一处」互为镜像：那是该改的没改，这是**不该改的改了** |
| `specs/gates/rv-ledger.txt` | RV 标注台账（reviewer-owned）—— 稳定 ID ＋ 最终状态；`--final` 以它为可比对象判「标注是否消失」。🔴 执笔方可增行，**不得改他域已有行的状态** |
| `scripts/appendix-e-owner-gate.sh` | 附录 E.1–E.4 全段认领闸 —— 「未认领恒为 0」须有 `owner_sp` 列承载；🔴 判「每行有没有主」，**判不了认领得对不对** |
| `scripts/evidence-fresh-gate.sh` | 证据包时效闸 —— 证据必须产自被评审的那个锚；🔴 **快照指纹一致只证明文件没被篡改，不证明内容是当期产出**，两者是不同的保证 |
| `scripts/superseded-claims-gate.sh` | 已撤销断言闸 —— 裁定改了引用点必须跟着改；🔴 判「清单内旧断言还在不在」，**判不了清单是否完备** |
| `scripts/gate-witness-gate.sh` | **覆盖见证元闸**（乙域跨闸硬契约第 4 条的**执行者**）—— 核每个闸是否给出「预期集合 · 实检集合 · 未知集合」。清单由 `speclint-gate.sh` **机器派生，非手写**。🔴 立件原因：**契约没有执行者 ＝ 没有契约**（本轮已有三个实例）。⚠️ **实测债务：20 个闸中仅 2 个合规、18 个缺见证** —— 故**暂未接入提交闸**，接入时机报归口方；**债务可见先于债务清零**。判【有没有见证】，🔴 判不了【见证里的数对不对】　🔴 **接入范围**（coordinator 2026-08-26 裁）：接入**侧分支** `fix/e-registry-and-18-1` 的提交闸/CI 链路（`scripts/speclint-gate.sh` ㉙ 段）；**不接主评审面硬提交闸**，主闸接入等并锚后再定。⚠️ 实现上**一律实跑、一律打印**，只有【是否阻断】随分支不同 —— 不做成「别的分支上不跑」，因为**一个不跑的闸和一个不存在的闸，在输出里长得一模一样**；非侧分支上 ㉙ 若为红，末行摘要必须改口（不得印「全过」）。棘轮四条阻断＋基线件＋逐闸 owner/到期见 `specs/gates/gate-witness-baseline.txt` 与 `gate-witness-debt.txt` |
| `specs/gates/GATE-CONTRACT.md` | **闸的返回契约〔通则〕—— 空集不得当通过**。🔴 立件原因（coordinator 由三域各报一角摆在一起看出）：RV-乙18 跳过 · ㉕ 缺面 · CI 不触发 · 附录 E 未登记，**四条不是四个缺陷，是一个** —— **被检对象不存在时闸返回 PASS 而非 UNKNOWN/FAIL，「空集」被当成「已检查且合格」**。定三态（PASS/FAIL/**UNKNOWN 不得折叠为 PASS**，默认 fail-closed）＋ 输出三要素（**受检范围** · 判不了什么 · 空集如何处置）。⚠️ **范围句是本轮普遍缺的那项**，边界句 ≠ 范围句 |
| `specs/gates/REASON-CODES.md` | **抑制/豁免的受控理由码与其机器可判后置条件**（乙域 2026-08-26 裁定归执笔方实现）。🔴 立件原因：理由此前是自由文本、闸只校非空 —— **执笔方曾以「跨行误命中」为由压住两处【行内就有】的旧断言而闸返 0**。**判据交给码，叙述留给人**：码不在表内即拒，码在表内则跑后置条件（如 `cross-line-false-positive` ⇒ **该行行内不得含被抑制短语**，正好拆穿那次编造的理由）。⚠️ 判**码与行形态是否相符**，🔴 **判不了豁免该不该给、也判不了码选得对不对** —— 归门方 |
| `specs/gates/superseded-claims-exceptions.txt` | 已撤销断言闸的**受控例外**（六列：`claim_id` TAB 被抑制对象 TAB 件 TAB 行指纹 TAB 理由码 TAB `approval_ref`）。🔴 立件原因（乙域 RV-乙18）：原以行内自由文本 `style-ok:` 豁免，而**闸不看理由、只看有没有那四个字**。v3 再修一层（乙域复验 @ `434b8fe`）：**「批准方」是一次字符串比对，不是一次批准核验** —— 填字面「terra」即放行、覆盖 `AUTHOR_SESSION` 即可自批。现改为 `approval_ref` 须解析到门方台账并四元组匹配。⚠️ **登记 ≠ 获批准**；**授权根不可得 ⇒ UNKNOWN(2)，不是 PASS** |
| `specs/gates/lib/exceptions.sh` | **受控例外公共库（四个使用点共用）** —— 行指纹（内容哈希）· 理由码后置条件 · `approval_ref` 解析核验（受保护 ref ＋ 不可变 commit）。⚠️ 冻结基线棘轮**已按乙域收尾裁定删除**（待批准＝拒绝）。🔴 执笔方会话名**写死在库内、不读环境变量**：否则「不得自批」这条规则的开关就握在被规则约束的一方手里 |
| `specs/gates/approvals/TRUST-ROOT.md` | 批准记录的**授权根**声明 —— 机制＝**丙（门方独占受保护 ref ＋ 不可变 commit ＋ 记录 ID ＋ CI 验可达）**，乙域收尾裁定精确版；甲（git blame）只是元数据非授权，乙（GPG）可日后叠加但无密钥信任/撤销根时不可单独作后端。🔴 **当前无任何 ACTIVE 后端**⇒ 已登记的受控例外与**依赖它的那个闸**一律 **UNKNOWN(2)**，均不得 PASS |
| `specs/gates/approvals/ACTIVE.conf` | 授权根的**机器读取面**（闸只读本件，不读散文件）。🔴 立件原因：上一版闸直接读 `TRUST-ROOT.md`，其「启用方式」**示例**的行首形态与真配置一样 ⇒ **文档示例把后端激活了**，而同一页还写着「当前零 ACTIVE」。**判据与被判物必须同源。** 当前 ACTIVE 0 条 / REF 0 条；注释行不生效（有反测钉住）|
| `specs/gates/reviewers.txt` | 门方会话名册（**工作区副本，仅供人读**）。🔴 判定时**不读本件** —— 名册取自受保护 ref 的**同一 commit**，因为工作区这份在执笔方写权内 |
| `specs/gates/lib/approval-fixture.sh` | 四闸反测共用的**批准夹具**（造门方独占仓 ＋ 受保护 ref ＋ 不可变 commit）。⚠️ 只供 `--selftest`，真实判定路径不读它 |
| `specs/gates/path-anchor-exceptions.txt` | 路径锚闸（`path-ok` 通道）的受控例外，格式同上。🔴 行内 `path-ok:` 自由文本**已不再构成豁免** |
| `specs/gates/status-consistency-exceptions.txt` | 状态一致性闸（`status-ok` 通道）的受控例外。🔴 原实现只校「理由 ≥8 个非空白字符」——**那是长度，不是批准** |
| `specs/gates/prose-style-exceptions.txt` | 体例合规闸（`style-ok` 通道）的受控例外。⚠️ `style-ok` 有**两个**使用者，RV-乙18 当时只改了已撤销断言闸那一个 |
| `specs/gates/gate-witness-baseline.txt` | 覆盖见证元闸（㉙）的**冻结基线**（`missing_witness=18` / `compliant=2`，乙域给定值）。🔴 上调＝放宽判据，属评审面；执笔方不得自行下调 |
| `specs/gates/gate-witness-debt.txt` | ㉙ 的**债务清单** —— 逐闸 `owner ＋ 到期里程碑`；🔴 **无 owner / 无到期即 FAIL**（乙域 2026-08-26）|
| `specs/gates/superseded-claims.txt` | 「旧断言 → 现行断言 → 撤销依据」清单，本闸的数据源 |
| `scripts/checklist-executed-gate.sh` | checklist 执行落账闸 —— 宣称「已按 checklist 执行」时机器要求 checkbox 已勾**且证据栏非空**；未落账可登记例外，但输出明说**这不是 PASS** |
| `specs/tools/foreign-version-check.py` | 正文他件旧版本号检查（analyze ④b）—— ④a 只读头块一处，正文里的假版本看不见；本脚本补正文面，只在与该件称谓同行时才判 |
| `scripts/prose-style-gate.sh` | 体例合规闸 —— 正文不得含评审过程痕迹（指向特定评审轮次/版本/评审方的表述）；含 **RV 评审标注**模式：轮内可标、收官须逐条落到 采纳/驳回(带理由)/挂账，且正文清零 |
| `specs/tools/pending-debt/main.go` | 「尚未双轴化」债的检测器（Go AST）—— **按效果判**：谁返回 `PendingCtx` 造的上下文谁就是债源，其调用点全部计入，helper 叫什么与判据无关（乙域 terra SA-12b）；按 `(domain, ownerSP)` 分组供闸逐领域棘轮 |
| `specs/tools/safe-replace.py` | RV 段免疫的批量替换（提交闸 ⑯a）—— 替换前遮蔽【RV…】标注段及其处置行，模式若**只**命中标注段则判误操作并拒绝（静默零命中会被读成「已清理干净」）。🔴 立件原因：闸 ⑯ 是**事后检测**，丙域 kimi 要的是「作用域排除 ＋ 防复发」，检测不是预防 |
| `scripts/conflict-marker-gate.sh` | 冲突标记恒零闸（提交闸 ⑲）—— 交付件不得含未解决的 Git 合并冲突标记。🔴 立闸原因：原型导出函数里提交了冲突段而 ①–⑰ 与 analyze 全绿（标记落在 JS 块注释内，语法不坏）。⚠️ 按【行首精确 7 字符】判：本仓 charter 件用 32 个 `=` 作分隔线，子串判据会全部误报 |
| `scripts/path-anchor-gate.sh` | 路径锚解析闸（提交闸 ⑳）—— 正文反引号包起的仓内路径须解析得到。🔴 立闸原因：#36b 的 reviewer owner 锚被写成仓内不存在的路径，人工判据落点指向空文件而 ①–⑲ 全绿 |
| `scripts/legacy-name-gate.sh` | 旧模型名棘轮（提交闸 ㉑）—— 受控符号集合的 **AST 出现数**，prod/test/comment **三类分别**只许降不许升，prod 终值 0。🔴 首版用 `grep -c` 数【含词行数】被甲域抓出：同行两次只记一次、删注释可抵新增生产用法 |
| `scripts/gen-review-state.sh` | 评审状态生成件（提交闸 ㉒）—— 从台账与闸实跑派生 `docs/REVIEW-STATE.md`。🔴 立项原因：评审转述与推锚已**交叉五次**，根子是「我推锚快于转述、而状态只活在消息里」⇒ 状态落进仓，门方 checkout 即可读 |
| `specs/gates/disputed.txt` | 待裁项 —— 门方判据与业主裁定相撞、执笔方不得单方面决定的条目。🔴 立件原因：争议只活在消息里，被原样转达三次而无人能判断其状态 |
| `specs/gates/reviewer-repro.txt` | 门方复现台账的数据（㉓）—— 每行：<发现ID> TAB <期望> TAB <门方原命令> |
| `scripts/title-version-gate.sh` | 标题/版本字段一致闸（提交闸 ㉔）—— 三件各自标题版本号须与版本字段一致。🔴 立闸原因：交付前实取发现**架构件标题 v0.33 vs 字段 v0.34**；闸 ① 的 hdr-state **只覆盖 SpecDoc**，架构件与原型不在其判据面内 ⇒ 属「断言作用域小于实际需要」 |
| `scripts/post-columns-gate.sh` | §18.1 列集一致闸（提交闸 ㉘）—— 列清单须**机器可唯一提取**，且【`POST-COLUMNS` 块 / 表格 / 原型自述】三处一致，差集**双向报出**。🔴 立闸原因（coordinator 实测）：同一节三法抽取得 19/19/21，原型另述一数 ⇒「以 §18.1 为准」无处落。**问题不是哪个数对，是判据与被判物不同源。**⚠️ 判三处一致，**判不了列定义对不对** |
| `scripts/workflow-coverage-gate.sh` | 触发面覆盖闸（提交闸 ㉗）—— CI 的 `paths` 须覆盖**闸的载体本身**（`scripts/` 脚本 · `specs/gates/` 基线），并核 `push` 与 `pull_request` 两处一致。🔴 立闸原因（ops 实测）：触发面曾只有 `docs/**`，闸的载体不在自己的触发面内 ⇒ 上调基线或删闸脚本，CI 一声不响。⚠️ **本闸过 ≠ 仓侧不可逆保护已落地** —— 触发面对了只是「红得出来」，是否为必过状态检查属平台侧配置，非 git 可见项 |
| `scripts/input-anchor-gate.sh` | 输入锚解析闸（提交闸 ㉕）—— §4.7.1 ⑥ 声明的输入锚须解析得到。🔴 立闸原因：⑥ 立面后，「范围不扩 / 共识不变」两条默认约束的判据**全压在锚上**，锚悬空则两条落空而正文看着合规。⚠️ **判锚是否解析得到，判不了陈述追不追得到锚**（后者归门方，E.4 #66）—— 两层不可互相冒充 |
| `scripts/dependency-ratchet.sh` | 依赖清单棘轮（提交闸 ㉖）—— §4.7.1a「依赖不增」默认约束的对应物，直接依赖数只许降不许升。🔴 立闸原因：**不声明即生效的约束若无事后可机器验证的对应物，就退化成「记得少加依赖」**——那是纪律不是机制。⚠️ **判数不判换**：等价替换（数不变而集合变）只提示、不判门，须人审 |
| `specs/gates/dependency-baseline.txt` | 依赖棘轮基线（㉖）—— `direct_count` ＋ 依赖名集合 sha256。🔴 **上调基线＝放宽判据**，属评审面，须随 spec §4.7.1 ⑤ 的突破论证一并提交，不得静默改 |
| `scripts/reviewer-repro-gate.sh` | 门方复现台账（提交闸 ㉓）——每条发现记**门方自己给的复现命令**与期望值，闸每次实跑比对。🔴 立闸原因：执笔方报「已修」时自拟的一行判据，多次证明的是**上一轮修复还在**，而非这一轮的洞已修 ⇒ **他问什么，闸就跑什么** |
| `specs/tools/legacy-name/main.go` | 上闸的计数器（Go AST）—— 逐 Ident 计数；注释按 **Go 标识符边界切词后精确相等**匹配（子串会让 `PendingTenantCtx` 再命中 `TenantCtx`）；扫描根不可用 / 遍历出错一律 **exit 2**（走不到就不能报数）；复合标识符（`tenantCtxKey`）不计：判**受控符号集合**，不判子串 |
| `specs/gates/legacy-name-baseline.txt` | 旧模型名棘轮的基线（㉑）—— prod/test/comment 已钉 `0/0/0`，任一旧标识回流即红 |
| `specs/fixtures/rls/setup.sql` | 人层 RLS fixture 建面（乙域 RV-乙12）—— 三 role 分离 · FORCE RLS · 逐命令 policy（写命令带 WITH CHECK）|
| `specs/fixtures/rls/t1-write-as-other.sql` | 负测①：`dw_app` 不得把 `owner_principal` 写成他人（🔴 `USING` 只管读、`WITH CHECK` 才管写）|
| `specs/fixtures/rls/t2-no-context-deny.sql` | 负测②：缺 `dw.principal` 时读**0 行**而非全部、写被拒（fail-closed）|
| `specs/fixtures/rls/t3-no-residue.sql` | 负测③：事务提交后同一物理连接不得残留 principal（`SET LOCAL` 而非 `SET` 的唯一理由）|
| `specs/fixtures/rls/t4-ddl-rights.sql` | 负测⑤/正测：DDL 权归属须实证 —— 🔴 `GRANT ALL ON <表>` **只给表级 DML**，`ALTER TABLE`/建 policy **要求属主身份**；dw_migrate 经**属主 role 成员资格**取得 DDL，dw_app 与「未切属主的 dw_migrate」均须被拒（乙域 RV-乙15）|
| `specs/fixtures/rls/logs/original-setup-run-20260825T151003Z.log` | **首版失败的原始日志**（13 个 ERROR）—— 🔴 它暴露了两处派工转述里没有的东西：①建面自检在全线失败时仍打印「通过样」；②NOINHERIT 那条**不是日志抓出的**（全文 grep INHERIT = 0），是推理得出。**失败的证据与通过的同等重要** |
| `specs/fixtures/rls/logs/pg17-noinherit-run-20260825T151003Z.log` | **实跑原始日志**（⚠️ 仓内 `.gitignore` 含 `logs/` 与 `*.log`，本件须 `git add -f`；**证据差点被静默忽略**）（PG 17.11，ops 代跑，逐字入仓）—— 「已实跑」这一事实的载体；EXPECTED.md 的实测对照表据此写 |
| `specs/fixtures/rls/RUN.sh` | 一键代跑（交 ops 执行）—— 🔴 立件原因：RV-乙15 的两问只能由**有 PG 的一方**跑出，执笔方与乙域门方均无 PG；本件把门槛压到一条命令 |
| `specs/fixtures/rls/EXPECTED.md` | 上述三条的**期望**输出。🔴 **尚未在真实 PG 上跑过**（本机无 psql、docker daemon 不可达）—— 期望≠实测，不得据此宣称已验收 |
| `specs/tools/rv-state-check.py` | RV 状态/时点一致性单次解析 —— 替代原逐行 shell 循环（47s → 0.49s）。🔴 立件原因：原实现每行起四个进程，七千行两万余次 fork，门方 `timeout 60` 跑不完。⚠️ 自带**时限反测**（合成 2 万行断言 <5s）：selftest 夹具只有几行，覆盖不到**输入规模**这一维 |
| `specs/tools/disposition-audit.py` | 处置兑现 triage（提交闸 ⑱，**不判门**）—— 对账「RV 标了采纳」与「正文真改了」。🔴 立项原因：R2 轮3–轮4 三域独立抓到四次「标采纳而正文未动／改在了别处」，**没有一次是执笔方自查出来的**。⚠️ 误报率高（首跑 4 命中 3 误报：「一律不入正文」是规则本身、「移出表格」≠「移入沿革」），命中须逐条人判；判不了没被「」引起来的诉求 |
| `specs/evidence/EVIDENCE.md` | 证据包产出（作者侧旁证，非门方结论） |
| `specs/evidence/collect.sh` | 生成证据包：每条断言附产出它的命令，供门方复算 |
| `specs/evidence/reference-index.md` | 引用索引 |
| `specs/gates/repo-packages.txt` | SP7a 仓储包允许清单（现含 `internal/repo`，甲域 sol 附条件授权）；加条目＝放宽判据，属评审面 |
| `specs/gates/boundary-baseline.txt` | SP7a 边界棘轮基线（`open` / `escape` 各记各的，不合并） |
| `specs/gates/status-stale-exceptions.txt` | 状态一致性闸的显式例外清单，每条须带理由与归属；例外是债不是豁免 |

## 三项与 §4.1 的对应

| §4.1 要求 | 本结构 | 状态 |
|---|---|---|
| `constitution.md` 治理原则唯一载体 | `specs/constitution.md` | ✅ |
| `/speckit.analyze` 跨制品一致性检查 | `specs/tools/analyze.sh` | ✅ |
| `/speckit.checklist` → `node.acceptance` | `specs/platform/checklist.md`（+ 安全 / 跨制品两份） | ✅ **三域已产出【并执行完毕】**（26＋18＋12＝56 项），按其重走的架构评审 **verdict 全 PASS**；⇒ §0.4 第 2 项已闭 |

## 用法

```sh
specs/tools/analyze.sh     # 跨制品一致性（件与件之间）
scripts/speclint-gate.sh   # 单件闸六项（提交时自动跑）
```

## 两条设计取舍，写明理由免得被当成偷懒

| 取舍 | 理由 |
|---|---|
| **正文不搬进 `specs/`**，用装载点指向 `docs/` | ① 搬动即改路径，作废三域已 PASS 的锚；② 拷贝一份即第二事实源 —— 本轮反复栽的正是这一类 |
| **checklist 不由本席代写** | 执笔方自备 checklist 自己勾 ＝ 自签门 PASS（§4.1 · §7.2 同一条纪律） |

⚠️ **`analyze.sh` 只报机器可判的不一致。** 语义层的「引对号引错义」它判不了，
那归人审（SpecDoc E.4 #60 覆盖不到项之①）—— 不宣称能覆盖它。
