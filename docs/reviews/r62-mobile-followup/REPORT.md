# S-R62-MOBILE 追加小活 M1＋M2 交件报告

- 任务来源：组长 2026-11-18T18:37:07Z「S-R62-MOBILE 复核：通过，附两项小活」第五节 M1／M2；开工回执 request_id `020a7b0876d4`（19:04Z）。期限 2026-11-20T06:00:00Z。
- 分支：`qwen38mvb4/r62-mobile`，在冻结 tip `51ac35943` 之后只追加提交；`95e48084`（冻结代码）及之前一律未改。
- 新增提交：
  - `33575cd34` test(r62)：M1 抽屉开合改语义断言＋有界等待；
  - `c1f83251d` test(r62)：M1 基线验证暴露 waitSessionFrame 既有 frame 树竞态，改有界轮询＋语义断言（见 §1.4 披露）；
  - `403c7f9f5` test(r62)：M2 五套改造件登记进全量 runner＋MUTATION 环境白名单；
  - 本 docs 提交（见交件信 tip）。
- 改动面：仅 `view-service/console-mobile-regression.js` 与 `tools/run-browser-regressions.py` 两个测试侧文件。`git diff 95e480843..HEAD -- internal/ cmd/ tools/scripts/ deploy/ VERSION go.mod go.sum` 为 0 行 ⇒ 产品字节自冻结代码以来零变化，② 的 1440 PC 像素级逐字节不变结论原样继承。

## 一、M1：mobile-no-drawer 刀红在判据文案

### 1.1 改前读数（② 证据存档）
`docs/reviews/r62-mobile/evidence/r62m-knife-mobile-no-drawer.log`：passed=5 failed=8，8 条失败全部是 `page.waitForFunction: Timeout 10000ms exceeded.`，无一条落在判据上。

### 1.2 修法（只改测试，判据不放宽、不删格、不加重试）
- `openDrawer`：点击前先语义断言「窄屏必须有可见的抽屉按钮 ☰」＋「aria-expanded 初值必须为 false」；点击后等待 nav.open 且移入视口，上界 3000ms（CSS 过渡 .2s 的 15 倍余量，console_auth.go nav 过渡 .2s ease）；超时不再抛裸超时，而是落到断言文案「点 ☰ 后抽屉必须展开（nav.open 且移入视口）——抽屉打开接线失效」；展开后再断言 aria-expanded=true。
- 新增 `waitDrawerClosed(page, why)`：遮罩点击／ESC／选中 Agent／选中菜单项共 5 处裸等待改为有界 3000ms＋各自判据文案＋收起后 aria-expanded 回 false。
- 抽屉格原有的「按钮可见」「默认收起 right<=0」断言保留；PC 格、waitSessionFrame 的 15000ms 时间线等待、全部业务判据不动。

### 1.3 改后读数（最终字节 sha256 `0284ad2283a0bade47c778f50482b657a193705b960861b0a53fe4a0a118d671`，锚 c1f83251d）
- 基线 ×5：`m1f-baseline-1..5` 全部 passed=13 failed=0，rc 全 0。
- mobile-no-drawer ×2：`m1f-knife-mobile-no-drawer`／`-2` 均 passed=5 failed=8；8/8 条失败全落在「点 ☰ 后抽屉必须展开（nav.open 且移入视口）——抽屉打开接线失效」；两份日志 `Timeout` 命中 0。
- 其余四刀判别力保持（均红在业务文案）：
  - mobile-tabs-wrap：11/2，「窄屏标签条必须横向可滚」×2；
  - mobile-input-wide：11/2，「Agent 管理工作区输入控件不得宽过视口」×2；
  - mobile-no-viewport：5/8，抽屉格 ×2 红在「viewport meta 必须生效（visualViewport.scale≈1）」，其余 6 格红在「窄屏必须有可见的抽屉按钮 ☰」（改前这 6 格是 10000ms 超时，顺带转为文案红）；
  - mobile-session-small-targets：11/2，「会话阅读页可见点击目标必须≥40px」×2（带实测尺寸清单）。

### 1.4 额外披露：waitSessionFrame 既有 frame 树竞态（c1f83251d）
- 发现：首轮基线第 2 遍（`m1-baseline-2.log`，与 M2 单跑并行负载下）红 1 格：`Cannot read properties of undefined (reading 'waitForFunction')`——iframe 已 attached 但 frame 尚未注册进 `page.frames()`，`find` 返回 undefined 后直接崩。
- 定性：崩溃行写法与 95e48084 冻结版逐字相同（本报告附 git show 对照），属既有竞态在负载下暴露，非 M1 新断言引入；首轮其余基线 1/3 与最终轮 ×5 均绿。
- 修法：有界轮询 frame 树（5s 上界，本地导航注册通常 <100ms）＋语义断言「会话 iframe 必须已注册进 frame 树」；15000ms 时间线等待与判据不变。首轮全部日志原样保留（m1-*），不与最终轮（m1f-*）混计。

## 二、M2：五套改造件登记进全量 runner

### 2.1 登记前单跑 ×3（env -i 白名单，与 ② 门同环境模式；五套文件字节在 51ac35943 与登记锚 403c7f9f5 之间零变化）
| 套件 | 文件 | 三遍读数 | rc |
| --- | --- | --- | --- |
| quota | console-quota-regression.js | passed=15 failed=0 ×3 | 0/0/0 |
| platform-admin | console-platform-admin-regression.js | passed=32 failed=0 ×3 | 0/0/0 |
| task-board | console-task-board-regression.js | assertions=87 floor=86 failed=0（K13 deferred）×3 | 0/0/0 |
| panel-pointerevents | console-panel-pointerevents-regression.js | passed=11 failed=0 ×3 | 0/0/0 |
| specs-theme | console-specs-theme-regression.js | 全 PASS（含 theme light/dark 读数行）×3 | 0/0/0 |

15/15 全绿 ⇒ 五套全部登记，无非绿项。日志 `m2-standalone-<套件>-1..3.log/.rc`。

### 2.2 登记内容（403c7f9f5，只改 tools/run-browser-regressions.py，＋6 行）
- SUITES 增 5 条（插在 console_mobile 之后）：`console_quota`、`console_platform_admin`、`console_task_board`、`console_panel_pointerevents`、`console_specs_theme`。
- OVERRIDE_ENV_NAMES 增 `MUTATION`（quota／platform-admin 的 env 形式删保护刀开关；机械扫描要求）；`PW_CHANNEL` 原已登记。task-board 默认 chrome 通道，本机 /usr/bin/google-chrome 可用，未改套件。
- `--selftest-env-coverage` rc0（`m2-selftest.log`）：未登记 process.env 输入仍被拒。

### 2.3 登记后全量 runner 一次（锚 403c7f9f5，51 套，19:15:42Z→19:29:16Z）
- 命令与环境：`env -i` 白名单（HOME／短 TMPDIR／venv PATH／GOPROXY=off GOFLAGS=-mod=mod／HUB_TEST_PG_STRICT=1／NODE_PATH／PLAYWRIGHT_BROWSERS_PATH／SOURCE_COMMIT=403c7f9f5…），`python3 tools/run-browser-regressions.py --receipt`；脚本 `evidence/run-r62mf-runner.sh`。
- 读数：**51 套，49 绿 2 红，aggregate_rc=2**。receipt `evidence/runner-full.receipt`（generated_at_utc=2026-11-18T19:29:16Z，commit_sha=403c7f9f5db02985fb981512b9b538f623c6b802，overrides=NODE_PATH,PLAYWRIGHT_BROWSERS_PATH），逐套 rc 全列于 receipt 与 `evidence/runner-full.log`。
- 新登记五套在全量内全绿：console_quota=0、console_platform_admin=0、console_task_board=0、console_panel_pointerevents=0、console_specs_theme=0。
- 本单相关格全绿：console_mobile=0（13/0，M1 最终字节）、console_browser=0、renderer_provenance_icon=0（现网热修 8cabb351 来源图标格）、renderer_retry_role=0（r60 ④ 重发格）、renderer_in_console_iframe=0、renderer_failure_dock_embedded=0、renderer_flood_stress=0。
- 两红（均继承抖，非本单回归）：
  - `r15_session_history_pagination` rc=2：`page.waitForFunction: Timeout 3000ms exceeded` 于 r15-session-history-browser-regression.js:96；
  - `r15_session_history_mutations` rc=1：`drop_history_node_move: wrong assertion failed`，底层同一 :96 3000ms 窗。
- 继承归因证据（本单未改任何 r15 文件）：
  1. `git diff 206d41b2..HEAD -- view-service/r15-session-history-browser-regression.js view-service/r15-session-history-mutations.js` 为 0 行，两文件与 main 逐字同；
  2. 同机况两锚对照 ×3（`r15cmp-*`，19:31:31Z→19:32:20Z）：翻页 r62 锚 rc=2/0/0，main 206d41b2 锚 rc=0/2/2；mutations r62 锚 rc=1/1/1，main 锚 rc=1/0/1——两锚同抖、main 不更绿；全部 10 份红日志各含且仅含同一 :96 3000ms 超时；
  3. 该窗即 S-BROWSER-FLAKE-WINDOWS 定因的「滚动状态翻转 3000ms 窗」，修法冻结在 415b9bf5（未合入本两锚）；② 交件 followup-R15 两锚对照（T3 0/2/0/0/2 vs main 0/0/2/0/2）已获组长 18:37:07Z 信 §三 接受为继承。
  - 另附全量后首次单跑（`r15-*-standalone`）亦红于同窗，与两锚对照一致，不冒称「单跑必绿」。

### 2.4 证据锚定说明
- M1 首轮（m1-*）跑在 51ac35943＋M1 工作树改动上，套件字节 sha256 `0d01a8e1…`（m1-suite-file.sha256）与提交 33575cd34 一致；最终轮（m1f-*）锚 c1f83251d，字节 sha256 `0284ad22…`（M1F.suite.sha256）。
- M2 单跑 ×3（m2-standalone-*）跑在登记前工作树；五套套件文件在 51ac35943 与 403c7f9f5 之间零变化（git diff 0 行），读数即登记字节读数。
- 全量 runner 锚 403c7f9f5（RUNNER.source），receipt commit_sha 一致。

## 三、合规
- 不改 VERSION、不推 main、不出发布件、不部署；95e48084 及之前提交未改写，只追加。
- 只动测试侧两文件＋docs；产品字节零变化。
- 证据在本目录 evidence/，SHA256SUMS 按仓库根相对路径，`sha256sum -c` 全 OK（日志经 git add -f 入库，绕 .gitignore 的 *.log）。
- 证据 130 件（SHA256SUMS 含本 REPORT 共 131 项，仓库根相对路径，`sha256sum -c` rc=0、131/131 OK）：M1 首轮 9 跑＋最终轮 11 跑（log/rc 成对）、M2 单跑 15 跑、selftest、全量 runner（log/receipt/rc）、r15 两锚对照 12 跑＋首跑 2、各段 start/end/source/done 锚、四份运行脚本、套件字节 sha。
