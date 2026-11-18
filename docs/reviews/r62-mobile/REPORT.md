# S-R62-MOBILE 交件报告（手机展示：窄屏可读可操作，PC 不变）

- 席位：qwen38mvb4.zzc；派单：组长 2026-11-18T14:14:27Z 第二项（业主：PC 版到手机上非常难操作和阅读）。
- 分支：qwen38mvb4/r62-mobile；基：①tip bef01c57c465cd0fca9eabd6e94b16ae919040e1（＝main 206d41b2＋①）。
- 产品提交：3700c995c20bcf93c458ea10f6c550f501854520（初版 888adc416 因 runner env 白名单 fail-closed 抓 MOBILE_SHOT_DIR 未登记而 amend 补登记，未推过远端）；测试适配提交：95e4808435ca03aa8361d5644fdcb4d3556a9dcf（既有 iframe/dock 两套件窄屏抽屉路径，见§四披露）；交件 tip 见文末。
- 期限：2026-11-20T06:00:00Z。不推 main、不改 VERSION、不部署。
- 门先报：request_id=e203cc94cb02（16:10Z）。

## 一、范围与实现
三页：Console 登录页（旧形＋新平台形）、工作台主页（菜单＋标签页＋Agent 管理）、会话阅读页。视口 390×844 与 360×780；PC 1440 对照。
1. internal/m8/console_auth.go 外壳：viewport meta；≤768px 媒体查询内——body 单列；nav 改 off-canvas 抽屉（transform 收起，.open 展开；按钮开，遮罩／ESC／选中菜单项关；aria-expanded 同步）；#tabs nowrap＋overflow-x auto；.group-workspace input/select min-width:0/max-width:100%；nav button／platform 链接／tab／关闭×／themeBtn／agentNavMode／logoutBtn／工作区按钮 min-height≥40（×与主题另 min-width≥40）。新增 #navToggle／#navMask 在 PC display:none；抽屉 JS 独立小段，PC 惰性。
2. internal/m8/console_login.go 旧登录页：viewport meta；窄屏样式全在 @media(max-width:768px) 内（PC 渲染逐字节不变，像素对照证实）。
3. view-service/renderer-canary-sse.html 会话阅读页：既有 768 媒体块内追加 .text-btn／.unlock-btn／#themeBtn／#clockStatus／#interruptBtn min-height:40px、themeBtn／interruptBtn／.unlock-btn min-width:40px。实测原 7 个可见目标 24～34px（historyOlder/themeBtn/clockStatus/copyUnlockBtn/shotBtn/interruptBtn/sendBtn），改后全≥40（measure 日志）。只抬最小高宽，不改配色结构；md-render-core.js 未触（sha 回写流程不适用）。
4. 新平台登录页本已达标（viewport＋44px 目标＋@media600），只验证不改。
5. 新增 view-service/console-mobile-regression.js（13 格）＋登记 tools/run-browser-regressions.py（45→46 套）。

## 二、新格与删保护刀
格（每窄视口六＋PC 一）：旧登录溢出 0＋码可见；新平台登录溢出 0＋可见 input/button≥40；工作台抽屉开合（默认收起 right≤0、按钮开＋遮罩可见、遮罩关、ESC 关）＋visualViewport.scale≈1；抽屉选 Agent⇒抽屉关＋iframe 会话加载≥100 行＋document 与页内横向溢出 0＋纵向滚动 scrollTop>0＋页内可见点击目标≥40；四 Agent＋Agent 管理共≥5 标签⇒#tabs overflow-x auto 且 scrollWidth>clientWidth＋document 溢出 0＋工作区输入不宽过视口；nav/标签/×/主题/退出目标≥40（取样≥10）；PC：navToggle/navMask display none、nav static 240px 贴左、body 网格 240px＋1fr、tabs wrap。
刀（全红各报因）：mobile-no-drawer（toggle.onclick=null⇒开合格红）；mobile-tabs-wrap（nowrap→wrap⇒横滚格红）；mobile-input-wide（min-width:0→500px⇒工作区输入宽过视口红；首版 320px 在 360 视口容得下不红，重锚 500px 并记因）；mobile-no-viewport（摘 meta⇒scale 0.398 红）；mobile-session-small-targets（摘会话页 min-height⇒目标格红列七件）。

## 三、PC 不变证据
1. 像素级：基锚 bef01c57c 外壳/旧登录 vs 本分支，1440×900，同夹具；page.clock.install 冻结时间＋screenshot animations:'disabled' 收敛瞬态动画（首差 9px 定因为历史加载条 ::before 无限动画相位，非布局）。工作台／会话／旧登录三页 PNG 逐字节全同（pc-compare.log＋六 PNG）。
2. 计算样式格：PC 布局原样（见§二）。
3. 既有六套 console 套件单跑全绿；全量 runner 46 套见§四。

## 四、门读数（逐名；原日志入 evidence/）
正式锚 T3=95e480843（env -i 白名单，HUB_TEST_PG_STRICT=1，SOURCE_COMMIT=T3；脚本 evidence/run-formal-T3.sh）：
- G1 m8 全包 -count=1 -p 1 -timeout 90m（17:06:47Z～17:14:38Z）：rc0，988 PASS／0 FAIL／11 SKIP；interrupted(9) 事件 0 条。整包全绿。
- G2 全量 runner 46 套不选格（17:14:38Z～17:27:13Z）：aggregate rc2＝45 绿 1 红。唯一红＝r15_session_history_mutations rc1（drop_history_node_move 期待断言被 r15-session-history-browser-regression.js:96 的 3000ms 滚动状态翻转窗超时抢先——与①交件同根因的继承抖族，归冻结 415b9bf5；本分支未触该两测试件）。console_mobile rc0（13/13）、console_browser rc0、renderer_in_console_iframe rc0（3/3）、renderer_failure_dock_embedded rc0（4/4）、r15_session_history_pagination rc0、renderer_flood_stress rc0。逐套 rc 见 evidence/formal-T3-G2-browser.receipt。
- G3 run-build-tests（17:27:13Z～17:38:48Z）：rc0，BUILD_TESTS_OK helper_profiles=2 python=27 shell=2；helper 闸 claude／codex 两 profile 各 1270（1261＋9）全绿。guide_staging 继承抖本轮未现（T2 轮曾现，补证见下）。
- Z-3 五轮（m8 整包 -count=1 -p 1 串行×5，evidence/z3-r1..r5，17:38:48Z～18:23:57Z）：
  - r1 rc1（986 PASS／2 FAIL／11 SKIP）：FAIL＝TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent（hub_test.go:3746 interrupted(9)）＋TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to（view_mention_route_test.go:420 回执=[]）——恰白名单两格。interrupted(9) 7 行：2 行在两白名单 FAIL 格内；5 行为 PASS 格期间异步租约释放噪声（TestViewDirectOutputCanStillMentionAgent 3 行、TestViewMentionRoute/overlong_request_id 2 行，两格均 PASS）。
  - r2 rc0（988／0／11）：interrupted(9) 0 行。整包全绿。
  - r3 rc0（988／0／11）：interrupted(9) 0 行。整包全绿。
  - r4 rc0（988／0／11）：interrupted(9) 9 行全在 PASS 格期间（TestGroupCrossOwnerExistingPolicyAndLimits/closed_additions… 3、TestGroupCrossOwnerViewMembershipAndQueue/known_offline_peer… 3、TestViewDirectOutputCanStillMentionAgent 3；三格均 PASS）。整包全绿。
  - r5 rc1（987／1／11）：FAIL＝FeishuOnce／Flapping（:3746）白名单格；interrupted(9) 1 行即该致 FAIL 行。
  - 汇总：整包全绿 3/5（r2／r3／r4）≥3 达标；全部 FAIL 均在白名单两格内，无白名单外 FAIL。PASS 格期间的租约释放噪声行即①交件已披露、组长已核收的 r61 继承竞态；本单 T3／main 聚焦×10 复证（evidence/followup-Z3-focused-*）：T3 10/10 PASS 6 行噪声、main 10/10 PASS 0 行噪声（①证据中 main 曾 3 行 ⇒ 两锚均现、非确定性）。本单产品 diff 不触租约／会话代码。
- r15 两锚对照（18:24:00Z～18:24:44Z，evidence/followup-R15-*；T3=95e480843 vs main 206d41b2）：pagination×5 rc 序列 T3＝0/2/0/0/2、main＝0/0/2/0/2，红轮全部同根因 r15-session-history-browser-regression.js:96 3000ms 滚动状态翻转窗超时；mutations×1 T3 rc1（drop_scroll_anchor_correction 期待断言被 :96 超时抢先）、main rc0（本轮抖未触发；①交件证据中两锚均 rc1 同根因）⇒ 同族继承抖，归冻结 415b9bf5（r61 集成后验证），非本单回归。
- G3 补证（G3c 18:25:08Z～18:27:11Z、G3d 18:27:51Z～18:27:53Z，evidence/followup-G3b/G3c/G3d-*）：guide_staging…zero_one_two 单格——共享隔离 HOME（/tmp/r62mh）下两锚均 rc1（同 test_guide_staging.py:654 断言：SECOND 帧带平台上下文前缀）；换新 HOME 两锚均 rc0 ⇒ 状态敏感、两锚同进退，非产品回归（S-HELPER-FLAKE-2 f7b32525 继承族）。python27／shell2 补跑 rc0（27 python＋2 shell 无 FAILED，PYTHON_SHELL_SEGMENT_DONE）。T3 正式 G3 本身 rc0 全绿。

### 四之一、过程披露（预检与首轮正式，全部原日志入 evidence/）
1. 预检（888adc416，evidence/precheck-*）：G1 rc1 唯一红＝TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent（hub_test.go:3746 interrupted(9)）＝Z-3 白名单格；G2 rc3 全套＝runner env 白名单 fail-closed 抓 MOBILE_SHOT_DIR 未登记（ENVIRONMENT_UNAVAILABLE）⇒ 补登记 OVERRIDE_ENV_NAMES（STALE_SCREENSHOT 同例），runner --selftest 过，amend 为 3700c995c；G3 rc0 全绿（helper 两 profile 各 1270、python27、shell2、BUILD_TESTS_OK）。
2. 首轮正式（3700c995c，evidence/formal-T2-*）：G1 rc1 唯一红＝TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to＝Z-3 白名单第二格（view_mention_route 420 一带）；G2 rc2＝46 套 42 绿 4 红＝r15 pagination rc2＋mutations rc1（继承，归 415b9bf5）＋renderer_in_console_iframe rc1＋renderer_failure_dock_embedded rc2；G3 rc1 唯一红＝test_guide_staging…claude_stream_json_user_frame_boundary_counts_zero_one_two（claude profile rc0／codex profile rc1，S-HELPER-FLAKE-2 继承抖，set -e 跳过 python27/shell2 ⇒ 补证见§四）。iframe/dock 后两红是**本单抽屉改动暴露的既有套件窄屏腿路径问题**：两套件在 ≤768 窗口（iframe 420 窗／dock 600、480 窗）直接点 nav 内按钮／顶层 ◐，而抽屉默认收起、目标在视口外 ⇒ page.click 超时。产品行为本身符合派单（抽屉默认收起、点导航自动收）；修在测试侧（T3 提交 95e480843）：窄屏腿先开抽屉再点；iframe-③ 点 ◐ 前开抽屉、点后按业主真实路径点遮罩空白处收起（主题不是导航、产品不自动收，navMask 会挡 iframe 内后续点击）。几何／对比度／可见性断言一字未改；PC 腿（>768）路径零变化。修后单跑：iframe 3/3（evidence/iframe-standalone-after-fix.log；修前 2/1 对照 evidence/iframe-standalone-before-fix.log）、dock 4/4（evidence/dock-standalone-after-fix.log）。
3. Z-3 处置：预检与首轮正式各命中一个白名单格 ⇒ 按 r60 五轮口径，正式锚 T3 上 m8 整包再串行 5 轮（evidence/z3-r1..r5），逐轮逐名报；≥3 轮整包全绿、FAIL 只许两白名单格（FeishuOnce／Flapping hub_test.go:3746 与 view_mention_route_test.go:420 一带）。interrupted(9) 行按上下文归属逐条列：致 FAIL 行与 PASS 格期间的异步租约释放噪声行分列。噪声行（session delivery lease release failed … reason=session_disconnected）即①交件已披露、组长已核收的继承性 r61 租约竞态（①证据 Z3-focused-r62/main.log：两锚×10 均 PASS、r62 9 行／main 3 行）；本单在 T3／main 两锚再复证×10（evidence/followup-Z3-focused-*）。本单产品 diff 不触租约／会话代码。若任一轮出现白名单外 FAIL 即停报。
4. r15 两格：本分支未触 r15 测试件；renderer 改动仅 ≤768 媒体块内 min-height/min-width，r15 跑 PC 视口不受影响。两锚（T3 vs main 206d41b2）各 pagination×5＋mutations×1 对照见§四读数与 evidence/followup-R15-*。guide_staging 继承抖同按既定口径：单格两锚复跑（共享 HOME 两锚同红、新 HOME 两锚同绿 ⇒ 状态敏感）＋python27/shell2 补跑（evidence/followup-G3c-*／G3d-*／G3b-*）。
5. 🔵 口径更正：门先报 e203cc94cb02 写「47 套」有误——main 基线 45 套＋console_mobile＝46 套（receipt suite.* 行 46 实证）。

## 五、截图证据
evidence/screenshots/ 14 张：login-old／login-newplat／workbench-drawer／session／tabs-workspace ×{390x844,360x780}＋workbench／session／login-old／login-newplat ×1440x900。路径与 sha 见 SHA256SUMS。

## 六、证据清单与交件坐标
- evidence/ 160 件：预检三门（precheck-*）；首轮正式三门全套（formal-T2-*）；正式锚 T3 三门全套（formal-T3-*，含 receipt／rc／时刻戳／ALL.source）；Z-3 五轮（z3-r1..r5 日志／rc／时刻戳）；r15 两锚对照（followup-R15-*，含 anchor 件）；G3 补证（followup-G3b/G3c/G3d-*）；Z-3 聚焦两锚×10（followup-Z3-focused-*）；iframe/dock 修前修后单跑；自验基线 13/13＋五刀＋六套 console 单跑＋会话页目标 measure＋PC 像素对照（脚本＋日志＋六 PNG）；截图 14 张；可复跑脚本 12 份（run-r62m-gates／run-formal-T2／run-formal-T3／run-z3-rounds／run-r15-followup／run-g3-followup／run-g3-freshhome／run-z3-focused／run-chain2..5）。
- SHA256SUMS：REPORT＋evidence 全件，sha256sum -c 全 OK（交件 tip 处复核）。
- 交件 tip：本 docs 提交，push 后 ls-remote 核一致、随交件信报；远端对象核：fetch 后 archive 解包逐件 sha256sum -c（读数随交件信报）。
- 密钥真值扫：evidence 全件 grep QWEN_API_KEY／sk-／api_key 模式 0 命中。
