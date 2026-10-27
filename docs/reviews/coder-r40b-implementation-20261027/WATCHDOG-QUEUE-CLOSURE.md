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

最终全套门、真实CLI与散列待结束收齐；togpt60独立交叉仍为上线必需门。
