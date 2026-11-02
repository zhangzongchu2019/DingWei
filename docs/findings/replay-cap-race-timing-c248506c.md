# FINDING：race 构建下既有批量淘汰时限断言过紧

依据组长 a7d6e937a78741ba8dfa4cb768479444 第3条判定。本单不修改性能阈值、不跳过该测试、不把整包 rc1 改报通过。独立后续处理，不属于 already_closed 产品修复。

固定基线 c248506cf120c3af2a06eeaeeae3038f596537b1；本单源码 d8e737a4104fd099de2810a36f9a8bdf759afc47。internal/m8/amr.go 整文件 Git blob 两版均 77662b96ea387e9e66232f2d510f94df50b5dd85；replay_guard_test.go 均 f118e29363a44920ed69aaac5026ffc63013405f；amr_test.go 均 d027fd4937563682ea050eda4ed03b05c5dc6afe。停机修复对上述路径 diff 为无。测试处理器直接构造内存 map/queue，不启动 Hub 生命周期工作。

原断言在 internal/m8/replay_guard_test.go:555：触顶批量淘汰必须≤50ms；后续1000次检查平均≤1ms（原值均未改）。完整 m8 race 自然完成2648.265秒、rc1，唯一失败为本格60.688101ms；无数据竞争警告。原日志 docs/reviews/hlc-primary-integrated-tail/m8-race45.log 第4783行起保持；SHA256 9f8423d547d5f4c6a1846999ed6df8808d12533c2cb46f1b46f9fba8018dadf3。前次10分钟超时另列，不覆盖。

## 同机单路对照

白名单环境、锁定helper venv、专用短HOME/TMPDIR、普通dev，无继承在线SH/DINGWEI/外部PG凭据。顺序a→b→c→d，每组同目标用例-count=10，不改产品/断言。逐次耗时与rc、实际命令、UTC、日志SHA见 ../reviews/hlc-replay-cap-control/RESULTS.json。

|组|源码|构建|通过/失败|触顶耗时范围ms|
|---|---|---|---|---|
|a-r47-plain|c248506c|普通|10/0|8.701180～9.728580|
|b-r47-race|c248506c|race|0/10|60.053852～65.525438|
|c-d8-plain|d8e737a4|普通|10/0|8.588711～10.282195|
|d-d8-race|d8e737a4|race|0/10|56.201929～63.444921|

两版普通构建全部通过，两版race均稳定超50ms；当前版race区间56.202～63.445ms，未劣于r47的60.054～65.525ms。按负责人事先判据，归既有race构建下性能时限不适配，不判本单回归。此结论只豁清本单归因，不取消该既有失败或宣称m8整包race rc0。后续性能测试口径/优化另行处理。
