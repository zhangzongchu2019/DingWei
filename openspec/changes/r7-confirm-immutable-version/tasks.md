# Tasks

仓库 wego-ai/dingwei；变更 r7-confirm-immutable-version；执行者 coder56solzzc.zzc 单写；起点 d1a7a686110c9721c61f403bb9cac4cae0b694ee。派单只给仓库、变更、任务编号及完整 commit。规格交审已通过；随后获本轮部分实施单，仅推进 T-09/10。完整依赖、实际 Git/ACL 集成及 T-03～08 仍未完成。

## 1. 规格交审

- [x] T-01 核固定决议与月计划、读取最小基线/context；编写 proposal/design/增量，每条需求附正反场景，形成 R7 全部需求与任务映射。
- [x] T-02 OpenSpec 1.16.0 / Node >=22.12 / 关闭遥测运行 `openspec validate --all --strict --no-interactive`；核改动仅规格、R11 锚不变；记录完整 tip/tree 后冻结交审。

## 2. 依赖与口径（待回写）

- [ ] T-03 等 prodops 核主体/ACL、仓库/commit/目录查询事实和凭据管理位置；按已批共用 R7-PM01 及三类豁免，补反例样本及预期、实际接口/时限/路径，复核本次派单后规格变化并 strict；未定不得实施版本有效性自动判定。 【乙；交接：实际 GitHub/工蜂主体ACL、目录查询事实及secret位置未核；prodops/提供方负责人补事实。】

## 3. 后续实施（须另派，不属于本单）

- [ ] T-04 实现 R7-001/002/006，S01/S02：合法事实与可见坐标、逐主体授权/拒绝、异常输入、缓存撤权和界面语义；同步相应正反验证，记录证据。 【乙；交接：当前 R7 bridge 默认503、提供方/ACL接线未完成；由实现席按核实接口完成并测撤权/异常矩阵。】
- [ ] T-05 实现 R7-003～005，S01/S02：带版本策略比较、四类实质变化、三类豁免及混合实质修改反例、无关变化、强推、历史不可读、并发/迟到与未知判定；逐格验证该失效确实失效、不该失效不误失效，并记录证据。 【乙；交接：缺真实版本策略读取与全场景失效判定，核心实现未完成；实现席补齐。】
- [ ] T-06 实现 R7-007，S01/S02：追加历史/幂等及确认不生成验收发布；执行该提醒确实提醒、该拒确实拒且正常路径不误拒的相应场景。 【乙；交接：缺R7真实提醒/拒绝路径和追加历史验收；实现席完成后交独立QA。】

## 4. 独立验收

- [ ] T-07 按 R7 全部场景逐个记录 PASS/FAIL/未执行/不适用与证据；四格场景逐格单列。用真实受控 Git 测试仓、独立权限主体与浏览器验证，不能以模拟通过替代真实权限验收；由质量保障组独立复核，非本席自验收。 【乙；交接：缺真实受控Git仓、独立权限主体与全场景独立复核；由 QA 执行。】
- [ ] T-08 获授权再做预发，复核源码未通过消息/文件服务交换、完整坐标审计、确认失效和 MR/委员会区分，另行申请 GO。未获授权不部署、不重启、不改线上配置。 【乙；交接：缺预发授权与环境GO证据；业主授权后 prodops 验证。】

## 5. 本次部分实施单（不代表 T-03～08 全部完成）

- [x] T-09 按组长授权实现 R7-001/002/005/006/007 的独立核心：不可变坐标/身份/时刻/含义、SQLite 持久化与幂等、全长 commit 的安全 Console 展示、委员会/MR 分离；无真实 Git 依赖时默认拒绝并明确剩余边界。
- [x] T-10 按 R7-003/004 提供可替换候选 A 策略且默认关闭，默认不同 SHA 待核对；覆盖四类实质变化、强推不转移、无关提交、未知/错版本读取及默认策略不启用；已批豁免由下方 T-11 追加；跑相关包、race、浏览器和 run-build-tests，记录精确结果后冻结本次交件。

## 交接记录

- 2026-11-01T02:09:23Z · coder56solzzc.zzc · commit a9619b296d822bc31979785f797be2ff4aecbc4a
  - 完成：T-01、T-02；内容 commit 如上。OpenSpec 1.16.0 / Node 22.23.1 / OPENSPEC_TELEMETRY=0，`openspec validate --all --strict --no-interactive` rc=0，8 项通过、0 失败；新增两能力最小基线/context 与两份增量，共 15 条需求、30 个场景标题，四格场景要求逐格取证。改动仅 OpenSpec；远端计划仍为 edadb623，R11 仍为 5c0fce04；新分支提交前远端不存在。最终 tip/tree 以承载本交接记录的冻结提交及交件坐标为准。
  - 阻塞：组长独立审阅、prodops 事实与共用 R7-PM01、R6-PM02；T-03～08 均未开始，所有产品场景未执行；无实施、预发或上线授权，strict 不等于业务验收。
  - 下一步：普通 push 新分支 opus50tc2k/openspec-r6-r7，核远端 tip 后冻结交审；先回写依赖与 PM 口径，再由组长另派实现。仅本段任务勾选/交接新增，未改内容 commit 的需求正文。


- 2026-11-01T02:26:32Z · coder56solzzc.zzc · commit a1435f54abad53839c8f69c0080c64ffb7258271
  - 完成：本次部分实施 T-09、T-10；不可变确认记录/幂等/原时刻、固定 SHA 与未知比较、可替换且默认关闭的 A 策略、独立 Console 全长 commit/委员会与 MR 区分、可信依赖缺失拒绝与 0068 迁移核验。Go1.22.2 四相关包全回归 rc=0；最新代码的 R7/平台入口/路由认证/迁移专项与 R7 三包 race rc=0；Chrome 页面模拟 HTTP 正反格通过；run-build-tests rc=0（两 helper profile 各 1087、90 文件、27 Python/2 shell）；OpenSpec 1.16.0 strict 8/8。证据 evidence/r7-immutable-confirmation/validation.json 说明完整包运行与后续源码改动的专项补验。初次系统 Python 缺 helper 依赖，改用既有锁定 venv；迁移账清空时重复 CREATE 问题已修复并反测。
  - 阻塞：T-03～08 保持未完成；主程序未装配可信委员会/工蜂主体 ACL/commit 验证器，实际记录读写默认 503，入口默认隐藏。R7-PM01 建议及 tasks/交接/archive 豁免未批准、不启用；候选比较不表示真实 Git/权限或全部 R7 场景已通过。R6 不实施，无版本升级、部署、重启或冻结激活。
  - 下一步：以承载本交接的最终 tip/tree 普通 push opus50tc2k/r7-impl 并冻结独立复核；先等 PM/接口事实并补真实集成、比较审计与全场景验收，再另派激活/预发。R11 与 R6/R7 规格冻结分支均不动。

## 追加派单：已批豁免（本轮）

- [x] T-11 追加已批 A 策略三类豁免、正反场景及固定策略版本；默认关闭。核真实提供方/委员会/ACL 适配仍未装配、迁移与发布控制器未改；相关验证与证据完成后冻结新 tip 交组长。旧交接中的 PM 待批为历史状态，不代表当前阻塞。

- 2026-11-01T02:45:32Z · coder56solzzc.zzc · implementation commit 90536f7d92d789bb7d70d35ab10a965a01305e65
  - 完成：T-11 三类已批豁免及策略 v2，默认关闭；specconfirm/store/m8 全包、specconfirm race、三刀变异及 run-build-tests（两档各1087，90文件，Python27/shell2）通过，strict 8/8。cmd 全包一次临时 PG 拒绝连接导致单例失败，该例原代码独立复跑通过；不把首次全量改记为成功。精确读数见 evidence/r7-immutable-confirmation/approved-policy-validation.json。
  - 剩余：T-03～08 仍未完成，真实提供方/委员会/ACL、比较审计、归档历史发现与完整生命周期待后续；无激活/迁移/发布配置变化或预发授权。最终 tip/tree 以承载本交接的提交为准，追加交件后冻结。

### 本次清账交接（2026-11-08）
- T-03：实际 GitHub/工蜂主体ACL、目录查询事实及secret位置未核；prodops/提供方负责人补事实。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ opus50tc2k/r1-r8-impl 397fafb1; docs/reviews/r7-impl/REPORT.md @ opus50tc2k/r7-impl f1d26de8。
- T-04：当前 R7 bridge 默认503、提供方/ACL接线未完成；由实现席按核实接口完成并测撤权/异常矩阵。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ opus50tc2k/r1-r8-impl 397fafb1; docs/reviews/r7-impl/REPORT.md @ opus50tc2k/r7-impl f1d26de8。
- T-05：缺真实版本策略读取与全场景失效判定，核心实现未完成；实现席补齐。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ opus50tc2k/r1-r8-impl 397fafb1; docs/reviews/r7-impl/REPORT.md @ opus50tc2k/r7-impl f1d26de8。
- T-06：缺R7真实提醒/拒绝路径和追加历史验收；实现席完成后交独立QA。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ opus50tc2k/r1-r8-impl 397fafb1; docs/reviews/r7-impl/REPORT.md @ opus50tc2k/r7-impl f1d26de8。
- T-07：缺真实受控Git仓、独立权限主体与全场景独立复核；由 QA 执行。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ opus50tc2k/r1-r8-impl 397fafb1。
- T-08：缺预发授权与环境GO证据；业主授权后 prodops 验证。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ opus50tc2k/r1-r8-impl 397fafb1。

### 本次清账交接（2026-11-08）
- T-03：实际 GitHub/工蜂主体ACL、目录查询事实及secret位置未核；prodops/提供方负责人补事实。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ opus50tc2k/r1-r8-impl 397fafb1; docs/reviews/r7-impl/REPORT.md @ opus50tc2k/r7-impl f1d26de8。
- T-04：当前 R7 bridge 默认503、提供方/ACL接线未完成；由实现席按核实接口完成并测撤权/异常矩阵。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ opus50tc2k/r1-r8-impl 397fafb1; docs/reviews/r7-impl/REPORT.md @ opus50tc2k/r7-impl f1d26de8。
- T-05：缺真实版本策略读取与全场景失效判定，核心实现未完成；实现席补齐。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ opus50tc2k/r1-r8-impl 397fafb1; docs/reviews/r7-impl/REPORT.md @ opus50tc2k/r7-impl f1d26de8。
- T-06：缺R7真实提醒/拒绝路径和追加历史验收；实现席完成后交独立QA。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ opus50tc2k/r1-r8-impl 397fafb1; docs/reviews/r7-impl/REPORT.md @ opus50tc2k/r7-impl f1d26de8。
- T-07：缺真实受控Git仓、独立权限主体与全场景独立复核；由 QA 执行。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ opus50tc2k/r1-r8-impl 397fafb1。
- T-08：缺预发授权与环境GO证据；业主授权后 prodops 验证。 证据边界：docs/reviews/r1-r8-core/REPORT.md @ opus50tc2k/r1-r8-impl 397fafb1。
