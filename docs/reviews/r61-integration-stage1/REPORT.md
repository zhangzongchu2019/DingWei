# R61 第一段七项集成交件

依据组长2026-11-18T10:20:18Z S-R61-INTEGRATION。基main206d41b26dab646c0e50a111eed5a45ccf7c72f6；冻结合并源 `38b9538dd2b1f1783aee6d7890ea03b83e33184e`，tree `8ac5ca18e7e92edc7a88e3182f6090d3135e7e78`。只本段七项；N-3、N-1b、N-1c仍待各自复核冻结，不在本段。VERSION保持main现值0.2.3.40；0.2.3.41留最后一段决定。不推main、不出发布件、不部署。

## 顺序与合并

每项合并前独立ls-remote核全值，原文和UTC见evidence/merges.json；均与派单相符。全部使用 --no-ff，一项一个merge，first-parent顺序如下。全部ort自动合并，无文本冲突、无手工改产品；每条提交说明如实记载automatic。第7项虽刀区邻接，合并后逐项核两处复活＋silent feedback均保留，白名单逐字等于429cf1dd，不单边覆盖。逻辑核验见structure.json。

| 次序 | 来件 | merge提交 |
|---|---|---|
| 1 | togpt60lunazzc/helper-admission-flake `bfaec0ba27060299fc87b0ad592db9fb5dc75b63` | `1283116e56a32a55f998e61f4782ab78185dfa56` |
| 2 | togpt60lunazzc/helper-flake-2 `f7b32525cd1b99d5188fbc3c8133018b6a8a8a82` | `625f70cf355be943012d40fe73e70e1d014eb67a` |
| 3 | togpt60lunazzc/r61-n1-busy-wait-v2 `638cb32477af230bedd2aba0bc547dee82b162ef` | `0c3cf7e48d6efa6bebd1861fd7680e9d0255f0c4` |
| 4 | qwen38mvb4/r61-n2-offturn-fold-v2 `8ae6b083dc3c3875bc3ddf8cc6ae53fd593ab298` | `f884c854dcd12af4d48a928d9e018db2ec5a0e66` |
| 5 | qwen38mvb4/browser-flake-windows-v2 `415b9bf5199e711b5323e21a0a147082a2108498` | `bbf29d8e8de2f2a6faa4de01d8eba01ae486c957` |
| 6 | qwen38mvb4/browser-followup `e26d7ee0046ef008cfa850a240d453869b56daa5` | `63f5c3d7b6dd12a25d3e01ea00b71f0a5ab2dc75` |
| 7 | qwen38mvb4/browser-mutant-revive `429cf1dd9c0cb59cb82191d5c7cfb0014f2420e0` | `38b9538dd2b1f1783aee6d7890ea03b83e33184e` |

## 完整门

所有门固定上述合并锚，普通用户、显式白名单环境、独立HOME/TMPDIR、PG strict、锁定Python，明确设置DW_DESKTOP_INTEGRATION_PYTHON。测试门使用build.sh --test-only，未制作发布件。SOURCE_COMMIT/命令/开始结束UTC/真实rc/原SHA见gates.json；PASS为日志行数，含count=3重复，不是不同场景数。SKIP逐名及条件见SKIPS.json。

| 门 | rc | Go顶层PASS行 | Go含子项PASS行 | SKIP行 | interrupted诊断行 |
|---|---:|---:|---:|---:|---:|
| build-canonical | 0 | 6 | 21 | 0 | 0 |
| m8-full3 | 0 | 2958 | 4965 | 87 | 0 |
| store-full3 | 0 | 1320 | 2214 | 15 | 0 |
| browser-full | 0 | 0 | 0 | 0 | 0 |
| strict | 0 | 0 | 0 | 0 | 0 |

浏览器全量47套全部rc0；套件内既有KNOWN-DEFECT观察原样保留在known-browser-boundaries.json，不把既有legacy中断缺陷冒称修复。r15 pagination与mutations当前均rc0，分页夹具字节等同415b9bf5；先前429来件全量中的两红仅按原作者证据列对照，本席不冒称复跑旧红。codex profile指定guide_staging格实际为ok，完整测试块见codex-guide-verdict.txt，两个profile各自数量/rc以canonical原日志为准。

## 环境失败不抹除

初轮同源canonical于10:24:02Z至10:25:55Z rc1：根分区耗尽，claude/codex各在创建/tmp/ht-*时Errno28，测试主体未完成，groups=[0,9]，不能记通过。首轮gates-enospc.json与build-canonical-enospc.log原SHA保留；未改产品/测试。第二轮同源canonical的两个profile各1275/1275且codex指定guide格ok，但最后shell段在TestBlockerAcrossRealHTTPHelperIPCMCP创建/tmp/rbt临时目录时再次Errno28，整体仍rc1，另见gates-enospc-shell.json与对应原日志。未把两个失败轮次拼成一次绿。仅将本席三套工具链和两份已停用Go编译缓存逐文件/符号链接比对后移到数据盘，保留原路径映射；无历史证据删除、不清理他席文件。空间增至约891MiB后整段完整重跑，同源最终读数如上。

## 交接

完成：七项顺序合并及本段作者门，全部来件锚已进入祖先链，原有r60 provenance保护保留。无本段实现阻塞；本报告不替代组长/独立复核或上线授权。下一步：按组长令待N-3/N-1b/N-1c分别冻结后追加，不提前合入、不抬版本。原始日志gzip只进Git，敏感夹具样式脱敏如有明确列发布/原SHA；无现网/预发动作、无在线探针、无他席正文。

最终文档strict：`b92d23a7a9230473819615ed40ae9f9e06c4adf6` 上validate --all --strict为43/0、rc0；原日志、命令、UTC与SHA见evidence/final-document-strict.json及同名txt。源锚38b的42/0与新增集成交接规格后的43/0分开记录。后续提交只封存证据，不再修改openspec。作者证据首提交为a0bec74e5，tasks已附完整不可变坐标。
