# 首轮冻结门：非 PASS（原样保留）

受测源 d6240358c5a3a7d22ef170b2f1f7e3e0f382679c，tree 8ddd3b8597c560cae5234125cf91819bb9459388，输入1893项均与Git blob相符，结束时源摘要未漂移。

通过：严格内嵌PG预热、vet、全仓Go、49项撤修（100次命令）、两版0.161.0／0.163.0真实owner与受控PTY、三套相关浏览器。所有原始stdout/stderr/rc与UTC均在本目录；不据此抹掉下列两项失败。

canonical rc1：既有 test_actual_hung_candidate_deadline_is_independent 给候选只余约0.2秒启动，未落marker后 FileNotFoundError；新私有端点用例未执行到。暂判负载下窗口不稳，未做基线施压对照，不称已坐实根因。未改该测试或产品guard。

helper rc1：891项中10项ERROR，均在新增NativeAlias夹具setUp的LocalPeer.bind，AF_UNIX path too long。夹具把64位物理名放到受TMPDIR影响的目录，较长TMPDIR触发内核上限；产品实际目标固定/tmp且两版门已过，不能把夹具错说成环境免验。后继只把模拟原生目录改到独立短私有/tmp目录，64位名称与所有检查保留，产品路径限制不放宽；在原同一长TMPDIR下42项已全部通过。

执行器另修为每命令独立HOME／TMPDIR，canonical在高负载并行任务前完整串行跑；helper、canonical不再共享外层HOME。后继重新冻结，所有要求的门完整重跑，最终以后继目录读数为准。原轮不改写为PASS。
