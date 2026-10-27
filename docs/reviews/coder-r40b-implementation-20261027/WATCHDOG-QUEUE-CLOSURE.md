# B4 watchdog：保留未知语义并释放队列

基于874ef48追加，响应组长在59b5479上的独立复核；不改写原提交，不部署或重启helper。

## 旧1019 PASS的入口与差异

旧命令从仓库根执行，Python为 `/tmp/coder-0232-locked-venv/bin/python3`（3.12.3），等价完整调用如下；原日志为 `r40b-helper-full.log`，1019项、437.257s、OK。

```python
import os, subprocess
base = {k: v for k, v in os.environ.items()
        if not k.startswith(('SH_', 'DW_', 'DINGWEI_', 'DATAD_'))}
base['PYTHONPATH'] = os.getcwd() + '/tools/sessionhelper'
subprocess.run(['/tmp/coder-0232-locked-venv/bin/python3', '-m', 'unittest',
                'discover', '-s', 'tools/sessionhelper/tests', '-p', 'test_*.py'],
               env=base, check=True)
```

**该入口包含test_unified_queue.py，但清除了SH_DRIVER，DriverAdapter默认codex。** 同一Python3.12.3、同一代码，修改前：清理环境的该文件14项PASS；只加SH_DRIVER=claude，该文件FAIL1，正是line315 timeout事件为0。这已复现，不能把旧1019 PASS用于证明实际Claude席位环境。另，本机系统 `/usr/bin/python3` 没有websockets，锁定venv有；这也会影响discover的导入错误与总例数，不能只对比897/1019数字。

B4对Claude加强正证据要求后，fake的processing/turn=...不构成admission；watchdog却只在admitted时运行，未确认接纳的processing路径因而无限占住current。这是实现缺口，不是仅有旧断言问题。

## 修复语义

- 对已经dispatch、尚无admission正证据的命令，也从固定起始时刻运行进展watchdog；超时持久化admission_unknown，不伪造turn或processed，释放current并推进下一件。
- 已确认admission的命令超时，保留原turn/admitted_at与injected证据，只记录transport终态未知。ACK使用既有unknown阶段，Hub保留injected并标记turn_terminal_unknown，禁止降成未注入或合成模型终态。
- 超时事件改成delivery_transport_unknown/outcome=unknown，不再冒称turn_completed。队列内部failed仅表示本次transport处理离场；旧测试仍钉住推进，outcome断言从failed改为unknown，是因为模型失败从未得到正证据。
- 迟到真实terminal仍补原turn与真实时间；不再因watchdog已离场而隐藏真实事件。原生terminal已持久化但driver poll仍旧processing时，按同件原turn的持久正证据释放，不再把它误当无终态继续堵队列。

## 可复现入口

新增 `scripts/test-helper-suite.py`：固定测试根与PYTHONPATH，显式清除席位配置继承，逐例verbose记录，driver profile可显式选择；不改任何席位env文件。

```sh
# 完整套件，重现原来“清理继承变量”的入口语义；逐例日志可确认包含watchdog。
/tmp/coder-0232-locked-venv/bin/python3 scripts/test-helper-suite.py > helper-full.log 2>&1
# 原阻断文件，两种profile都必须过。
/tmp/coder-0232-locked-venv/bin/python3 scripts/test-helper-suite.py --driver claude --pattern test_unified_queue.py
/tmp/coder-0232-locked-venv/bin/python3 scripts/test-helper-suite.py --driver codex --pattern test_unified_queue.py
```

以上完整入口只对测试子进程清理环境；不是生产env变更。原系统Python入口存在的依赖/基线失败不以本入口冒充消失，交叉复核须同解释器、依赖与profile对齐。

## 初步判据

- 原文件：claude 14/14、codex 14/14；持久化/迟到真终态/陈旧poll回归16项PASS。
- 去掉watchdog的current释放，保留unknown事件：rc1，红在“watchdog未知离场必须推进下一件”，实际仅投递one，two未投递。
- Hub store相关10项PASS；浏览器unknown不冒称完成、迟到真实终态原位更新，46条实际断言PASS。

取证截止 `2026-10-27T11:30:29Z`；受测产品代码 `dd0dd42f37bc9b1d033f49c83237a8cb98e8de43`，tree `24dfc3f9e170cd1303944ee912ebbc41edc66b55`；最终报告追加提交只改文档。togpt60独立交叉仍为上线必需门。

## 最终读数：不把不同环境合并成一个PASS

| 验证 | 实际结果 |
| --- | --- |
| Claude专项：持久化／进展＋原文件 | rc0，32项（18＋14） |
| 原文件Claude profile重复 | 3轮各14项PASS，逐轮rc0 |
| 原文件Codex profile | 14项PASS；含Guide边界的Codex相关57项rc0 |
| 去掉watchdog释放 | rc1，红在“watchdog未知离场必须推进下一件”，仅one投递，two未投递 |
| 严格m8＋store完整门 | rc0，顶层PASS1039／FAIL0／SKIP7；含子测试1550／0／7；m8 334.962s，store 36.356s |
| 实际CLI／Hub／helper／View | 5组15格全部PASS，含此前Codex B admission格与Claude显式命令 |
| 原ACK三刀负控 | rc1分别红在判据2、4、8；不改断言或字面匹配delivery ID |
| go build ./... | rc0，Go1.22.2 |
| 标准构建入口，真实HOME上下文 | rc0，实际执行Python69组／shell2组 |
| ACK浏览器 | assertions=46，failed=0 |
| 完整helper，私有测试进程HOME | **rc1，1032项，434.537s，1个清理错误，不能标为全套PASS** |
| 最近完整helper，真实HOME（d334） | rc1，1031项，435.760s，3个模块清理错误；原日志留存 |

同步收尾修正：释放current放到本次同步进度簿记／输出之后，避免depth=0时同步收尾仍在写文件。持久原生终态路径在离场前也完成admission事件与回调；同时要求原turn、admitted_at、processed_at、有效outcome全部存在，历史processed空字段不能当作正证据。

### 尚未关闭的非watchdog断言异常

最终私有测试进程HOME全套的唯一错误：`GuideRealAdapterBoundaryTest.test_claude_stream_json_user_frame_boundary_counts_zero_one_two`，`test_guide_staging.py:618`退出`TemporaryDirectory`时，`rmtree`报`OSError: [Errno 39] Directory not empty`。无watchdog推进断言失败；**未归因、未称为既有、未放宽清理断言，也不继续循环试到绿色**。此前一次私有HOME运行在另一个Guide临时目录退出处报同形错误，原日志亦留存。Guide业务及该测试文件本次未改。

真实HOME全套的3个模块清理错误均为`HomeIsolation.finish()`报告`sessionhelper_inbox-coder56solzzc.db`发生变化，涉及event_bridge_replay、file_upload、sessionhelper；来源未归因，未忽略安全断言。额外私有HOME运行只给测试子进程临时home，并不改席位环境或暂停在线helper，原有HomeIsolation断言仍启用。

构建入口在私有HOME上下文另有一次升级deadline探针失败（expected75／actual0），没有修改该测试；最终上表标准入口使用原真实HOME上下文并rc0。另一份扩大到Guide的Claude profile相关57项有1个即时admission回调期望失败；该mock为processing/turn回执，不是Claude正证据，已作为profile差异保留，不混入32项专项PASS，也不冒充完整Claude profile通过。

因此只能报告“watchdog原阻断专项与严格Go门自测通过”；完整helper门还有上述清理异常，不能宣布r40b全门闭合或上线。仍待组长／togpt60独立复核与业主后续授权。

负控维护：原生持久终态现在可直接释放current；终态关联负控保留第一个已settle原生回合作为设置屏障，只对后续工作撤掉终态关联，仍要求红在判据4。不匹配字面delivery ID，不改断言；否则该旧负控会先破坏A设置并提前红在判据2，失去单独定位判据4的作用。


## 私有测试进程HOME完整入口（与真实HOME结果分开）

此入口仅隔离测试进程，不写任何席位env文件，不关闭或重启helper；没有删改、忽略安全断言。原真实HOME失败日志与本入口错误一起保留。

```sh
/tmp/coder-0232-locked-venv/bin/python3 - <<'PYTHON'
import os, subprocess, sys, tempfile
with tempfile.TemporaryDirectory(prefix='watchdog-final-suite-home-') as isolated_suite_home:
    test_environment = dict(os.environ)
    test_environment['HOME'] = isolated_suite_home
    result = subprocess.run([sys.executable, 'scripts/test-helper-suite.py'], env=test_environment)
    raise SystemExit(result.returncode)
PYTHON
```

锁定venv为Python3.12.3、websockets15.0.1；系统Python3.12.3本机没有websockets。旧1019总数与组长系统入口897总数不能脱离依赖／profile直接比较。

## 证据SHA-256

本机目录`/data/ai-dev-data/r40b-implementation-20261027-evidence`，通过Git仅交换报告与坐标；没有源码／制品对象存储上传。

| 日志 | SHA-256 |
| --- | --- |
| `r40b-watchdog-before.log` | `7668e8d674824a95f430662f03d8bcad3a69c4b466411997e8134fd7a3368d3e` |
| `r40b-watchdog-claude-before.log` | `bf8904a1922a3771cd427a4354d73a60182c2bba155e95342bb033d6f129bdd9` |
| `r40b-watchdog-target-claude.log` | `1f6f3ffe98418f5c2d942de6b16fbf4ce9ea94673b10d525374ef59ab2f28505` |
| `r40b-watchdog-target-codex.log` | `d12b28cb9bcf8868e4a92ca3f6548d42afa198b3c5c43f452e4d774f3bf31b9b` |
| `r40b-watchdog-closed-claude-32.log` | `adffe88e135e2238b2a49f499d3845ae6ef167d3452bb4946247c958a8d17a94` |
| `r40b-watchdog-closed-claude-repeat.log` | `50dc713d4b40eeb2aaf47ddf9066315565e0fe29676ab0091bcf5e913677e02e` |
| `r40b-watchdog-release-mutation-final.log` | `168637ccebfb8f2d5827483b189f249cf209673549d4aeffc4d1a732d1733874` |
| `r40b-watchdog-finalization-codex.log` | `6e1b4eef236ab04b52686f722d32618818848c0f1f4ed66dc2ef25d0c442d60b` |
| `r40b-watchdog-finalization-focused.log` | `d8584373798273ee9f4f81bf392fb110eb10eccb141e95b0e74cb21c17e49803` |
| `r40b-watchdog-final-helper-isolated.log` | `07ffc5b38b9e5397674459189137695e891c9d8163e050a65a4ee225197e49f7` |
| `r40b-watchdog-release-helper.log` | `f471957225a3ca46761d3ccf2f3882d35f6df7b1d0d456036a0bbe47a4ba5ca9` |
| `r40b-watchdog-isolated-helper.log` | `c2fa21029c5b24e271bb2ec495fa1c01ecc6fab6dba98673de8801c80401024e` |
| `r40b-watchdog-closed-go.log` | `5da6dcf14a032fc68b3836f9c2fd5aadd55f036e3e2c01949c6a07ec122847f0` |
| `r40b-watchdog-closed-build.log` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `r40b-watchdog-canonical-build-tests.log` | `e4f49600378fabc1802149640e9209018ad0628fddb0efe5691a5408d4ce5795` |
| `r40b-watchdog-closed-build-tests.log` | `d57181434dc96b0093a57af337f7808b1fb4a23891afbe71b63ed66bfde93549` |
| `r40b-watchdog-browser.log` | `f04b48d99b885a7aea50f59867ee57d5237dc653d1654fd40192bedb8da85e74` |
| `r40b-watchdog-closed-mutations.log` | `150d7022892296fb4201de2d79e7e4da7916b8e2b36733ba19f3b475325f9c01` |
| `watchdog-closed-mutations/drop-recovery-reconciliation.log` | `1f67fbefc911e017c17980fe34bc7e57322567c2e334b2ea2ccf3d0801464089` |
| `watchdog-closed-mutations/enqueue-as-admitted.log` | `f8673eeded45734e3083cd15a2d5680f24dd132b4b9f73e7def5499971d1dfe9` |
| `watchdog-closed-mutations/return-as-terminal.log` | `75e5781cff984ff19eb59183bc0c227439e4697515be2a486ff300c93df4dbb8` |
