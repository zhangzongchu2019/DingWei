# r58 集成冲突与语义核对

五次 --no-ff merge 均无文本冲突。顺序和完整第二父见 merges.json；基线 main 153bac4dfe6794a8f2d9438eb4ac4e76d6aa400d。

| 编号 | 组合 | 核对与处理 |
| --- | --- | --- |
| R58-C01 | helper-bound-thread＋turn-attribution | 第4项已把 deploy/test_build_entrypoints.py 的文件断言从93改94；第1项只在既有文件加11例，第4项新文件加2例。runner expected 本来就是独立 discovery 动态值，无待修的硬编码用例数。单独钉值提交记录93/1168→94/1181，合并树两profile发现1172+9，定向72绿；保留动态发现机制。 |
| R58-C02 | hosted-test-time＋claim-test-sockpath | hosted 全局写者 TestUpgradeUnknownPlanNotSwallowed、TestReconcileSeatTimeoutDoesNotStallBatch 仍串行；其余有界6槽并行。第5项 m8 socket夹具和canonical短TMPDIR保留，不触碰hosted产品；最终tip跑 -race -count=3。 |
| R58-C03 | 根绑定与M2 | 子lane不继承根require；broker白名单不改。根绑定源码不额外优化，保持冻结160ab322内容；重复校验调用是否实际多读不在本集成顺手改变。 |

不改 VERSION、不改已合提交对象、不并入未列产品项。冻结证据日志原有行尾空白单独披露，增量检查不遮盖。
