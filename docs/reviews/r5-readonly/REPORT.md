# R5 T-03～T-06 交件

R5 新阅读区只从受控 Git 托管提供方读取 openspec/ 普通 Markdown：逐主体绑定与实时仓库 read 核权，ref 先固定完整 SHA，正文/目录/内部导航固定版本。功能默认关闭；未配置凭据文件与受控仓库 ID 时没有 Console 入口，读取接口 503。工蜂适配器不可用、失败关闭，不使用 SSH 或其他凭据。绑定只供平台管理员维护并留审计，绑定本身不授予仓库权限。本单没有创建真实绑定、读取真实令牌、调用真实提供方或部署/重启。

## Git 坐标与范围

- 仓库：git@git.code.tencent.com:wego-ai/dingwei.git；只推工蜂。
- 分支：opus50tc2k/r5-impl。
- 起点：3fd26ba67ebc90db4f35449e6968c4f22f89666b（r45）。
- 首个普通合并：a7c24704（父 3fd26ba6、68c178a5）；规格分支 opus50tc2k/openspec-r5-r11，包含 d1a7a686。
- 产品实现：a118455e31b95685a839f944472e97d22c920a60，tree 404d5b66d4ec426058a9ee8d2dd93af53ccb2ecb。
- 安全测试补充：0e62fcbdccb365ff036f694b79f5b756d00d997b，tree 7c9f824a3e81c8d304db7e0e76139ed51dd5bff1；只补测试和变异 runner，无产品差异。
- 超限 HTTP/浏览器反馈测试子提交：166bef7a1d50bd1a176edd75039801cbcfe6d7ed；资源界限 fixture 收紧后最终测试源：f8112829401bfcad34f46c97441cf0e9fe1a37f8，tree a30a2ae80dfaa41137cd51a19700c5f962a4ed7c；均无产品差异。
- 最终证据提交为以上子提交，SHA/tree 由交件消息报告，避免文档自引用 SHA。原始读数、来源 manifest 与本报告同分支提交。
- T-07/T-08 不在本单；不迁移提供方、不变更版本、不实现 R6～R10、不写 Git 内容。

## 实现与事实

prodops 事实来自 review/prodops/r5-facts e8b14b69（08:37–08:43 UTC），逐项写回 R5-A01～06、design、context。数字仓库 ID 为身份；核发现响应 id，并在读前/读后 ACL 核验前以 canonical name 再核 id，拒绝旧名称被占用。发现接口可跟随受限 HTTPS API canonical 重定向，内容/ACL 接口重定向一律拒绝，跨 host 不访问。GitHub blob 读取核 SHA、size、实际长度、Git blob 摘要和 UTF-8；tree 先按 size 拦 2 MiB 以上文件。symlink 120000 和 submodule 160000 不显示为可读文档、不跟随。

GitHub 真实 HTTP 适配器在 internal/specread，所有 transport 测试均是模拟。实现采用 /commits/{ref} 与 Git blob JSON，两者官方文档核对见 proposal 补充 API 小节；prodops 实测是 git/ref 和 Contents raw，不能混写成这些新增调用也已真实验收。

recursive=1 截断丢弃局部结果，再以不可变子 tree SHA 逐级读取；仍截断或超本地界限则失败。单文件 2,097,152 字节；API 并发 4、单 API 10 秒、整次读取 15 秒、单 JSON 响应 8 MiB、tree 最多 10,000 项、回退最多 256 子树调用。逐次读取主/二级限速响应头，含发现重定向响应。无内容缓存、无权限缓存；读前及交付前分别重新核绑定与权限，错误返回不附先前正文或目录。

新页面不改公共 Markdown core，使用独立 DOM 文本策略：标题、代码围栏、文档/外链导航；图片仅替代文本，原始 HTML/CSS/属性作为文本，链接严格筛选，CSP 禁止资源/对象/iframe 加载。其余 Markdown 语法保留可读原文，不把 tasks 勾选/归档变成可写动作或验收发布记录。相对文档允许在 openspec/ 内归一化父目录；源码只经用户点击安全的同 commit 提供方 HTTPS 链接前往查看，服务端不代抓。

配置：DW_OPENSPEC_READ_ENABLED、DW_OPENSPEC_GITHUB_CREDENTIAL_FILE、DW_OPENSPEC_GITHUB_REPOSITORY_IDS。凭据只读服务端受限普通文件，不出日志、响应、前端；逐次读取支持轮换/撤销。管理员 GET/PUT/DELETE /api/console/openspec-bindings 由 Console principal + 当前 active platform_admin 守卫保护；修改另核 Origin 与严格 JSON，并事务写绑定和审计。迁移 0070 无真实绑定种子。

## 验证读数

| 验证 | 结果 | 证据 |
| --- | --- | --- |
| OpenSpec strict | rc0，12/12 | raw/strict.log |
| 规范 scripts/run-build-tests.sh | rc0；BUILD_TESTS_OK helper_profiles=2 python=27 shell=2 | raw/build-tests.log、build-tests.rc |
| 完整 internal/m8 最终补跑 | rc0，175.440s | raw/packages-final.log、packages-final.rc |
| 完整 cmd/workpulse 最终补跑 | rc0，176.052s | raw/packages-final.log、packages-final.rc |
| 完整 internal/store 修复后 | rc0，61.465s | raw/store-final.log、store-final.rc |
| 新提供方/HTTP/存储定向测试 | rc0 | raw/final-focused.log、credential-error-tests.log |
| 新 R5 浏览器 | 最终定向 rc0，19 检查，含超限文案/不可变来源链接和 70,000 / 71,680 / 2,097,152 字节完整末尾；全套时此前 18 检查通过 | raw/browser-r5-final.log、oversize-http.log、browser-suite.log |
| 防护撤除变异 | 40/40 断言转红，均 rc1；编译错误不计有效 | raw/mutations-final.json，各 mutation 日志 |
| 全浏览器 | 26 套：24 rc0，2 旧失败，aggregate rc2 | raw/browser-suite-receipt.txt、browser-suite.rc、browser-suite.log |

完整 Go 四包首轮 aggregate rc1 因新增 0070 的 ledger-loss 补账缺口；已复用精确 DDL preflight，修复后 store 整包通过，另补 ledger-loss/DDL drift 定向测试。原始失败保留，不将首轮全包日志改写为 rc0。m8/workpulse 修复后又补跑完整门通过；最后仅增加超限/有效资源 fixture 测试，新增测试单独通过。全部产品字节与 a118455e 一致。

全浏览器在 a118455e 产品源上运行，使用明确披露的 NODE_PATH、PW_CHANNEL=chrome 与既有 Python venv，环境剥离 SH_/DINGWEI_/DW_/DATAD_。新增 R5 套已注册规范 runner。两个旧失败：renderer_0206_file_render 的 R6 授权/query 夹具、旧金额/磁盘来源断言；renderer_flood_stress 的 iframe 定位。它们的脚本与 r45 基线逐字相同，修复子片 coder56solzzc/browser-baseline-fix 3b16caeb 已单独复核冻结，本分支不重复改该子片。r46 集成应一起带入并核最终门禁，不将本次 24/26 写成全绿。C2 既有 T166 行为按原套件处理，原日志保留。

变异汇总逐 guard 列测试名与 rc；覆盖身份/绑定/撤权、仓库 allowlist 与稳定 ID/旧名复用、版本与路径、symlink/submodule、tree 截断/坐标/上限、blob 坐标/摘要/size/UTF-8、限速、管理员/Origin、默认配置、凭据文件权限/错误脱敏，以及浏览器 HTML/图片/链接/路径/commit/晚响应。无缓存的顺序读测试覆盖失权、服务中断和跨仓结果不串用。第一轮资源上限测试未区分格式失败、晚响应测试时序不足，已收紧并保留旧汇总；后一次 33/35 的两个无效变异分别修正未使用变量与重定向 fixture 后有效转红，再补五项配置/凭据防护。最终计数只合并有效断言红，不用基础设施失败凑数。

规范 build 首次用了系统 Python，四个 helper 模块因缺依赖未收集，初始日志保留；换回已存在的 /tmp/r31-build-check-venv 后完整 rc0。浏览器首次 Node 模块对应的浏览器件缺失，随后本地固定 Playwright 1.56.1 使用已存在的 Chromium/Chrome；未改产品来绕过环境问题。

## 保留缺口与后续门

令牌完整授权 scope 无法由 API 响应证明；工蜂专用只读令牌未建，相关 API/撤销流程未核；Console 主体与 GitHub 账号尚无真实绑定。服务令牌可读不能替代逐主体授权。T-07 真实受控仓库/获权与失权主体测试和 T-08 预发另单；本轮模拟通过不冒充真实权限继承、安全现场验收或发布许可。保持默认关闭，冻结交独立审阅，随 r46 集成。
