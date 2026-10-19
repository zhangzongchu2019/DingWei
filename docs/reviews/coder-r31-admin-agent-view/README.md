# R31 A14：管理员全量 Agent 只读视图交件

本增量在 f547f025407f4e1fea7943a19826a6093e750d52 上落实组长 aa0ffb5dee1a 的 REWORK；先前 f547f025 是基线，最终候选以本目录随附提交为准。作者自测，等待 togpt60 独立审查；未部署、未访问43。

## 行为与边界

生产路由 GET /api/console/platform/agents 默认100、最多200，Agent ID 游标分页。只允许有效 Console 人类 cookie 对应的 active platform_admin；普通用户、manager、停用/降权/过期/吊销会话、Agent bearer（包括与管理员 cookie 混用）均不能读取。HTTP 和存储分别核实时角色，PG 在同事务用户行共享锁保护下读取及写审计；审计写失败不返回部分页面。

全用户库存投影所有者、全名、自有/托管、来源IP/节点名、在线与生命周期状态；节点只取 node_name，不透传托管账户或凭据。通用 Agent JSON 未扩大。普通本人列表保持原所有者围栏与来源隐私。平台管理增加只读列表、刷新、下一页；托管入口关闭时管理员仍可查看；manager 无入口。页面清除过期/拒权数据并丢弃迟到响应，不后台轮询制造审计。

无 DDL；复用0103与 audit_log，未新增迁移账本。PG/Mem 使用同一新增契约；异常 owner 引用保留记录但不编造简称。现有 R31 整体回退前提不变。设计入口 docs/designs/S-R31-ADMIN-AGENT-READ-VIEW.md。

## 检查与复跑

环境：本机自托管容器，Go本地工具链，临时隔离 embedded PG；浏览器 Playwright 1.56.1 与系统 Chrome 151.0.7922.108。浏览器使用实际 Go 渲染的 Console HTML 与本地 HTTP 夹具；HTTP 权限测试使用真实 N3.Actor 会话解析，PG 契约和审计故障测试使用真实临时数据库。这些读数不证明生产已启用或独立验收。

- ADMIN-CHECKS.json：13个被测源码/测试/入口文件运行前后 SHA一致；正对照与恢复对照通过。14个 Go overlay 撤修必须 rc1 且在指定测试/标记失败，编译失败或 panic 不算红；浏览器正对照6/0、三个指定撤修红、恢复6/0。
- 权限撤修覆盖 Mem与PG角色/active、HTTP独立门、bearer混cookie；功能撤修覆盖分页上限、IP响应、节点响应、Mem/PG读取审计与审计失败即拒；隐私撤修覆盖普通列表所有者围栏与 bearer来源遮盖。
- 页面撤修：manager错误获得菜单、IP/主机列消失、丢失下一页游标，分别要求指定断言红。
- full-go.log：identity、identity/httpapi、m8、cmd/workpulse 全包；使用 R31_R30_BINARY 本地旧二进制执行兼容格，严格 embedded PG，不接外部数据库。
- build-tests.log：仓库既有构建门 python49、shell2。
- hosted-browser.log：32/0；console-browser.log：20/0；node-browser.log：8/0。覆盖新增页面对既有 Console、托管管理与节点页面的影响。
- boundary.log、dependency.log：棘轮未增加；这不表示历史未清债务已归零。

复跑新增指定撤修：设置 NODE_PATH 为可用 Playwright 目录、PW_CHANNEL=chrome，运行 python3 tools/r31-admin-agent-checks/check.py --out 新的空目录 --browser。脚本默认仅用本地隔离 PG，所有源码撤修用 Go overlay，不修改受测工作树。

MANIFEST.json 记录本增量源文件、测试、设计和原始证据的字节数/SHA256，不包含自身；提交后从 Git blob 重新核验。父提交中的旧证据保持历史锚定，不改写为本增量全量验收。
