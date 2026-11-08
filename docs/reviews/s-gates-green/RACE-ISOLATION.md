# race分片隔离与核对口径

依据组长2026-11-08T07:02:56Z五项条件（request_id6458a0db4eaa，认证收件箱dw-dlv_df861fe1aee16ad4c234c61d80177ed9；07:03:34Z定向重投dw-dlv_5b4c933d4e01e02a18befd372bddde59）。

store以go test-list全部379个顶层用例按轮转分8片，每片独立go test进程，每项count3，无t.Parallel；TestMain与flag/包级变量属于各进程，未跨进程共享。既有资源使用t.TempDir、MkdirTemp唯一前缀；旧二进制兼容/子进程探针按原条件跳过，重启父用例传独立临时DB。根包测试无固定监听端口或跨片共享可写目录；被测payload里的/tmp/COMMAND是验证不执行的恶意字符串，不是共享输出夹具。Go编译缓存仅由Go工具链管理，不作业务测试状态。

取证搜索：rg -n对应t.Parallel、/tmp/、127.0.0.1固定端口、os.TempDir、MkdirTemp、WriteFile固定路径、Chdir等模式，仅检查internal/store根目录测试（不把migrate/pgtest子包的额外限制冒充本包行为）；逐项复看task_dispatch_compat_test.go、feishu_sticky_compat_test.go和TestMain。各片片内串行，隔离临时数据库与环境，动态包全局设置不会被其它片看见。extseat另跑完整包race3，PG按已有pgtest每夹具独立数据目录/端口，未借store分片代替。

raw/RACE-SHARD-PLAN.json保存清单及分配；race-list-all.txt和race-list-union.txt的diff -u结果须rc0且race-inventory.diff为空，无重复。raw/RACE-FINAL-REPORT.json逐片记真实rc与顶层PASS/FAIL/SKIP，三项合计必须379×3=1137，每个顶层run=3。raw/RACE-SKIPS.md列所有实际跳过（含可能的子用例），条件来自Go JSON输出。

原始全包race-count3仍独立保留；若由执行者在分片完成后结束，只针对已核实属于本次运行的PID，交ORIGINAL-RACE-DISPOSITION.json及原日志，不计作rc0。该处置不影响已逐例核齐的分片覆盖。
