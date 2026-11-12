# S-R59-INTEGRATION 验证入口

任务来源：opus50tc2k.zzc开工令，source_message_id 8ced30a24b24468a87212a34da6f6429，实际入箱request_id dw-dlv_112a287d4bbb26da325fd9be336c0618。写者01a1cf05-9d3d-75e2-9233-eb9d75994424，锁/tmp/coder56-r59-integration.lock；普通uid1001，截止2026-11-12T18:00:00Z。不部署、不重启、不构建发布件。

## 提交与冲突

- helper冻结首父c0fecb12ed86003ff2d29a1c0902cfb16bc3f8c7。
- View冻结第二父e0de23713606d3e89f1bf578d268550e70d4786d。153bac4d为3c9f2722祖先已机器核验。
- no-ff merge c56556baabbd0f0b4f886cb3886c8865e37b0058，tree cd2e5dec858c221acd77d4453bd4a232d67ab73c；父序严格为以上两项。
- version d26a342d08d28bcae7fb393d50dba12b8359ba8f，父merge，仅同r58 c5f0c101的四文件抬至0.2.3.39；页面无版本字面值。
- test infrastructure a8fcb1ae921dcdb5685f781a7410a9b863f082e3，父version；全量browser环境自检发现STALE_SCREENSHOT漏登记，补登记可选键，不设置该变量、不绕过自检或改产品。

renderer仅两处冲突：保留helper busyWait norm两行并接View unknownSinceMs/hidden；保留helper busy等待stageText并接View visibleStageText。独立生成blob 259ae2593386d1371f8442f9bb8e4027e7b6ee88，SHA256 8d39be9b7f9cf635da5cf5c4757a2e8030224a5ded9c1aa4f5c1aaa018a57d71。组长本机试合未推送，已在开工ACK请求精确blob供比对；未取得前不冒称已同值。独立定向delivery-status20/20、model-admission26/26均rc0。

## 验证范围

最终产品树固定后执行：两种PATH完整run-build-tests（含均无codex/claude）、Go1.22.2下internal/m8全包与cmd/workpulse/internal/version/internal/mcpgw、无筛选全量browser runner（Node22）、OpenSpec1.16.0 strict、diff检查。所有子进程使用env -i等价的显式白名单环境、lock venv与HUB_TEST_PG_STRICT=1，临时HOME/TMP、合成协议或独立本机Hub/PG，不使用真实账号/在线席位。测试内部临时编译夹具不作发布件。

完整原始日志在/tmp/r59-cf05/raw；交件时填真实读数、跳过名及原文/完整日志hash，不把尚未结束的门当通过。后续只追加证据文档，不改已测产品源。
