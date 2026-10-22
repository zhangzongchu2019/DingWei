# 第二轮冻结门：非 PASS（原样保留）

受测源 b4fe9504e41e3d38a6b3a3e109655cac60269823，tree f0b47aa10ab1c4e70e1052fe09cebe038c22f387；输入1893项与Git blob一致，结束时未漂移。

已通过：helper891项全部OK；旧hung-candidate格在串行canonical中通过；49项撤修全部指定红／恢复绿；两锁定版真实owner及受控PTY；vet；三套浏览器。

未通过1：canonical执行到deploy/test_build_entrypoints.py时，新增第60项未同步该合同中的59计数及成功标签，报60 != 59。这是本件接线遗漏，后继同步60并把test_private_endpoint.py加入必接线集合；没有改旧hung-candidate测试或guard。隔离预检：修后0，撤新接线1（0 != 1），恢复0，见development/canonical-wire-preflight。

未通过2：Go唯一失败TestNotifySocketFirstStopPreservesNewListenerAtReboundPath，在首次bind即invalid argument；实取拼出的Unix路径108字节，尚未进入新旧listener清理断言。执行器为每命令TMPDIR增加层级，把已有长用例名推到Unix上限；首轮较短TMPDIR全仓已过。后继只把Go专用TMPDIR设独立短私有/tmp目录，产品与Go测试不改。

后继还把握手后清空验证超时精确限定到private connector，保持legacy自定义socket默认超时语义；相应private quiet-stream撤修仍覆盖。重新冻结后全仓、helper、canonical、原生、撤修、浏览器完整再跑，本轮不改写PASS。
