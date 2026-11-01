# Proposal

## Why

现有 Console 只会读取数据库旧 Spec，不能把仓库 OpenSpec 的固定版本直接呈现给研发成员。全面切换要求以 Git 内容为唯一依据，因此必须补齐继承仓库权限的只读浏览，并在内容进入页面前校验仓库坐标、路径和资源边界，防止仓库文本、恶意链接或远程图片绕过授权与浏览器安全约束。

## What Changes

- 在 Console 中读取经授权仓库、完整 commit SHA 下的 OpenSpec 文档，并持续显示仓库/版本/路径；分支仅用于选取版本，显示结果必须钉住 commit。
- 只读呈现主规格、context、进行中与归档变更的 proposal/design/specs/tasks；保留 tasks 勾选原状，不允许 Console 写回，不由勾选或 archive 判定完成。
- **BREAKING**（仅新 OpenSpec 阅读区）：收紧 HTML、链接、图片和路径规则；不自动加载远程图片，不以现有公共渲染器直接输出作为新安全契约。
- 补 `console-spec-reading` 代码最小基线/context 与增量；不实现 R6～R10、能力时间线、Git 写入或新的旧格式编辑器。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `console-spec-reading`：扩展 CSR-001 到不可变仓库坐标读取，对新阅读区替换 CSR-002 的安全策略；新增 CSR-003～007 的权限、路径、失败边界和只读语义。最小基线本轮补齐，增量仍是未来行为。

## Impact

涉及 Console 新阅读区、服务端仓库托管提供方只读适配与授权、Markdown 输出及文件/tree 读取；不将 OpenSpec CLI 引入 Hub 构建链，不把源码送入文件服务或消息正文。依赖 prodops 提供仓库托管提供方 API 事实与用户/Agent 权限映射。规格审过之前不编写产品代码，不把待确认接口当成已可用接口。

## 来源

唯一需求来源为委员会决议：[OPENSPEC-SWITCH-DECISION-20261031.md](https://git.code.tencent.com/wego-ai/dingwei/blob/c2d9c9fdd0416828739f7042d2ceb9bc71f603c6/docs/decisions/OPENSPEC-SWITCH-DECISION-20261031.md) 第 4～6、10～15、17、19～22 条。
就绪范围：[就绪计划 R5](https://git.code.tencent.com/wego-ai/dingwei/blob/c2d9c9fdd0416828739f7042d2ceb9bc71f603c6/docs/decisions/OPENSPEC-SWITCH-READINESS-PLAN-20261101.md)。来源 commit c2d9c9fdd0416828739f7042d2ceb9bc71f603c6；实现基线 a8bb5e5847884b62ba01f6f48fbce3772e2d8b04。

## 提供方事实回写（T-03）

依据 prodops `review/prodops/r5-facts` / e8b14b69 的 `docs/reviews/prodops-r5-facts-20261101.md`（2026-11-01 08:37–08:43 UTC 实测）。改名、转移与超限 HTTP 行为只是该报告引用文档的推断，不冒称实测。

- R5-A01：GitHub `GET /repositories/{id}` 返回数字 id 与当前 full_name；目标实测 id=1441460280。身份按数字 ID，严格核响应 id；只允许 api.github.com HTTPS，同 host 重定向亦须核坐标。当前 full_name 仅作该次 API 路径/来源链接，不是永久身份。删除、失权不回退同名仓。
- R5-A02：`GET /repos/{owner}/{repo}/collaborators/{login}/permission` 已实测可用。绑定含 Console principal、provider/instance、GitHub 数字用户 ID 与 login；核响应 user.id/login 和有效 permission。管理员受控维护与审计；每次读取及交付前重新核绑定与权限。无绑定/无 read/不可验证一律拒绝；不把服务令牌权限当用户权限。现网尚无可信绑定，本单不写真实绑定。
- R5-A03：实测 git/ref/heads 返回完整 object.sha；实现用同仓 `GET /repos/{owner}/{repo}/commits/{ref}` 支持 ref/tag 与完整 SHA，核完整 SHA，后续 git/commits、trees、blobs 全部只用 SHA。该 commits/ref 接口为实现选择，模拟验证，未补做真实 API 验收。实测 main=6ed4d1e8… 无 openspec，规格分支 68c178a5… 才有目标目录，不自动回退版本。
- R5-A04：git/commits 固定 tree.sha；git/trees/{sha}?recursive=1 递归无分页，须核 truncated；截断时丢弃局部结果，逐级非递归子树读取，仍截断/越界则失败。blob 120000 symlink、commit 160000 submodule 拒绝且不跟随；仅 openspec/ 普通 Markdown。先按 tree.size 执行 2,097,152 字节上限，再按 blob SHA、size、实际长度、Git blob 摘要验证。实测 Contents raw 可取正文，实现选用 Git blob base64（模拟覆盖，非真实接口验收）。
- R5-A05：逐次读 X-RateLimit-Remaining/Reset 与 Retry-After，含二级限速；令牌只从服务端受限文件读取，配置缺失整个功能不可用。prodops 实测凭据管理位于宿主机 /etc/dingwei/credentials/；本单不读取/复制该真实令牌。响应不能证明细粒度令牌完整授权范围或仅只读，须由所有者独立审计；工蜂专用只读令牌未建，不借 SSH。实现有界值：单文件 2 MiB、单响应 8 MiB、目录 10,000 条、截断回退至多 256 子树请求、API 并发 4、单 API 10 秒、整次读取 15 秒；不缓存内容/授权，限额是本地保护值，不冒称提供方实测容量。
- R5-A06：独立 DOM 文本 Markdown 呈现，不复用或放宽公共渲染器。图片只显示替代文本；HTML/CSS/属性作为文本，外链只允许无凭据 HTTPS，同仓文档导航固定 SHA。工蜂 API 身份/权限/tree/撤销仍未核，适配器不可用并失败关闭；Console 主体与 GitHub 账号未绑定。上述缺口保留为 T-07/T-08 前置，不阻止本次已派默认关闭的模拟实现。

## 待 PM 决策（本轮全部已决）

- R5-PM01 **已决 A**：业主 2026-11-01T02:23:43Z（workpulse；组长转达单 8455a73321d3）；原话要点「按建议已批」。首期所有图片仅替代文本，不自动加载。
- R5-PM02 **已决**：业主 2026-11-01T02:3xZ（workpulse；组长补充单 cf25af1ea2f34d24a776718848d26172，原始时刻仅精确到该范围，不补造分钟）；原话要点「首期只开放 openspec/ 目录，代码经 context.md 链接到仓库托管平台查看」。平台只读取该目录允许的 Markdown；源码链接由用户主动前往提供方查看，不扩展平台抓取范围。
- R5-PM03 **已决**：业主 2026-11-01T02:3xZ（workpulse；组长补充单 cf25af1ea2f34d24a776718848d26172，原始时刻仅精确到该范围，不补造分钟）；原话要点「单文件上限 2 MiB，至少保证 70 KB 文档完整读出」；2 MiB = 2,097,152 字节，恰好上限允许，超出拒绝，显示「文件过大，请在仓库托管平台查看」并附获权的同仓同 commit 来源链接。tree/分页/并发/超时等非单文件限额仍待提供方实测，不再把单文件上限挂为待决。
- R5-PM04 **已决 A**：业主 2026-11-01T02:23:43Z（workpulse；组长转达单 8455a73321d3）；原话要点「首月管理员维护 Console 用户↔工蜂用户绑定表，平台以只读令牌逐主体核仓库权限」。按补充单统一改为 Console 用户↔按提供方用户绑定；受控管理员维护、可审计，平台仍须用只读令牌逐主体核权，不能以服务账号权限代替用户授权。Agent 身份另行核实，不由用户绑定表自动授予。
- 托管迁移**未定**：业主考虑工蜂转 GitHub；不据此执行迁移或改变实现。
