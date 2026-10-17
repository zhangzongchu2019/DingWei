# S-HOSTED-CLAUDE R2 — packager 修复交审

只交 R2，实现与作者本地定向资格完成，待 opus50ts 独立复审；不是整体 Claude/R3 PASS。

仓库 `git@git.code.tencent.com:wego-ai/dingwei.git`，分支 `review/coder/hosted-claude-packager`。
基线精确 `bab04755c8f452b955196114a8ebb42ac28a3c25`。
产品/定义/测试钉 `a46db36e668ef2c1c4dbfbc910fbcefdf22612f0`，tree `dd9083514f2251196f6796a0d984ca4c66b745df`；其后仅本目录证据。
正式 R2 需求来自 `review/opus50ts/seat-handoff-20261014@039c6e12` 的 `docs/reviews/opus50ts-hosted-claude-8f5529ad/REVIEW.md`。
相对基线只改 Claude 镜像目录：prepare_context.py、build-image.sh、test_claude_start.py，新增 context-map.json、PACKAGER-CONTRACT.md。没有修改公共 H3、canonical 接线、node/helper、launcher、Dockerfile、锁或 combo。

## 修复

1. Git 中提交 context-map.json，H3 锚定 r25 `031c4284cab5c0401ae09d0180eeede1a32c7833`/tree `73b73d9c5d78f33116d2d6023450955734aa810e`，运行锁SHA `5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0`。使用真实 H3 receipt 的 commit/tree/lock_sha256/files 格式和真实名称，拒绝输入自行选择另一源码锚点。实际 Git 对象核对得到47个 helper 导出文件、6个 H3 定义文件、3个锁定 deb 条目、46个待丢弃的 Codex native 文件计数；不是构建了真实上下文。
2. Claude Dockerfile、launcher、CLI wrapper、`.dockerignore` 以及 Claude package lock 必须从指定的精确干净 Git HEAD 读取。CLI 新增 --repo/--commit；wrapper 必填 CLAUDE_SOURCE_COMMIT。映射明确 H3 start.py 的来源以及 Claude 覆盖文件的源码路径，修掉了以前输出错误 Dockerfile/错误 guard 文件名/普通 dockerignore 文件的问题。R3 后续改动合并入所选精确 commit 后会被按 Git 字节选取，不需把某个旧 launcher hash 硬写进代码；H3 公共锚点不会随意自动漂移。
3. H3 整棵树必须与 receipt 文件集合及每项字节SHA匹配；源定义和完整新布局 helper 文件按锚定 Git 比对；wheel 身份/版本/哈希按锚定运行锁；两种 deb 的文件集合/哈希按锚定 deb 锁。重新给被污染的材料生成 receipt 不能自行授权。静态/helper/wheel/deb模式须与真实 H3 producer的0644一致。
4. 输入所有祖先目录用 dirfd/O_DIRECTORY/O_NOFOLLOW，叶文件用 O_NOFOLLOW/O_NONBLOCK、fstat常规单链接类型检查、有界读取和前后元数据一致性检查。拒绝符号链接、硬链接、FIFO/socket/目录冒充文件、额外空目录、路径逃逸/别名和重复 JSON/归档文件。只保留一次读取并验证的字节；不 copytree、不二次按未检查路径取输入。
5. 输出为新私有目录，通过已检查的 parent dirfd 创建。只写明确映射和白名单材料，子目录 no-follow、文件 exclusive 创建；旧输出不覆盖，失败仅删除本次新建输出。输出 receipt 包含 source commit/tree、H3 锚点及 receiptSHA、mapping/Claude锁SHA、所有常规文件的SHA/mode/type。

只复用 H3 的 start.py、helper/wheels/runtime-debs/security-debs。Codex Dockerfile/launcher等和全部 Codex native 都不进入输出。未使用的 Codex native 只核类型、canonical目录/数量和与H3 receipt的SHA一致，不假称认证这些已丢弃二进制的发布方。Claude native另按Git锁SHA512/SHA1校验并安全展开。未下载真实包体、生成真实制品或运行任何 helper/guard/Claude。

## 作者精确本地读数

白名单环境和已有锁定Python，私有 HOME/短TMP，全部合成离线材料和私有Git，不访问Docker/网络/PG/真实凭据。

| 检查 | 读数 |
|---|---|
| Claude 文件全部 fixture | 15项 rc0，5.543s；其中10项packager、5项原launcher/metadata基础fixture |
| 未改 H3 definition/start 回归 | 36项 rc0，2.097s |
| canonical 入口契约 | 11项 rc0；inventory25/marker49+2保持，无新增测试文件或接线修改 |
| wrapper sh -n | rc0 |
| 七项窄源撤修/恢复 | 各 baseline0 / withdrawal1 / restored0，全部是真实断言红、无编译/环境红 |

最终 Claude log SHA256 `b21ab7014418672dbb3005a4bd9a9f77f4ce33ed8144d97655f20d6d0b59a623`。
七刀分别是 Git 内容绑定、receipt字节清单、wheel哈希锁、deb哈希锁、叶文件nofollow、错误复用base Dockerfile、撤掉固定Docker前拒绝。测试断言未改；nofollow撤修被合成secret读取尝试trap拦住，没有读取真实秘密。每刀只选择一个用例，0用例不会记绿。源文件逐字恢复。

正格不是凭空手写一个只有start.py的假receipt：测试在私有合成Git仓库、合成wheel/deb/Node/tar上调用**真实H3 prepare_context producer与package_release映射**。只将其硬编码生产锁SHA替换成合成锁SHA；其余Git/文件/哈希/格式验证函数均真实运行，不mock内容校验。真实公共start.py字节进入staged输出但不执行；R3会另外测真实staged guard与凭据前拒绝合同。

污染负格涵盖修改H3 Dockerfile/guard/helper/wheel/deb后自重算receipt、缺失/额外/坏receipt、类型/链接/祖先重定向/模式、NPM归档链接/逃逸/重复、脏源/错源/外部锁不匹配等。正确组成与污染组成都驱动真实shell wrapper：正确组成仍固定exit2，污染在创建输出前拒绝，Docker trap从未命中。描述符100次检查计数不增长，失败/已存在输出不破坏原数据。

未跑完整canonical或Go/PG：本件限定R2 Python定义，未改这些实现或canonical集合。上述入口契约不是完整canonical 49+2 PASS；最终两线合并树及其完整门归负责人另交审。旧5项launcher fixture仍有原mock guard，绿不代表真实CLI/认证/MCP/R3已合格。

## 归属、证据和清理

SOURCE.json核15个Claude目录源钉、5文件差集；claude-start.py/claude.py/verify_inputs.py/Dockerfile/锁/combo和目录外对象与bab逐字不变。旧两个context用例用正确完整H3正格和更广负格替代；其余5个原方法AST未改。REAL-GIT-MAPPING.json是实际r25源码对象读取，不是生产包体验证。raw/exploratory-*是冻结前探索绿，不计入本精确源读数；final/control logs归a46。所有manifest键为仓库相对路径。

fixture产生的临时合成source tar/上下文都由TemporaryDirectory清理，未留制品或上传任何二进制。自有worktree32133818B、HOME/TMP清为0，0自有进程，未用Go cache。主源码与Git证据保留。run.py为本席路径/环境的作者复现实录；重新复跑须创建自己的私有目录，不能复用CLEANUP中已删除的HOME/TMP/mutant。

候选仅R2代码/本地验证，待独立审查；R3与其旧文档ABI/真实CLI/guard验收由togpt56terrats负责。最终合并树另审，verified=false与Docker前固定拒绝不变；不授予构建、宿主、网络、凭据、试点、部署GO，现网及dataconn四席冻结不变。
