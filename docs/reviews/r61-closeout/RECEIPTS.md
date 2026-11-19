# R61 结项凭据（openSpec 方案甲第 1 步清账用）

发布提交：产品 4a349ee59a1d8d7b8b9c562c462bded2f8b0f192（tree 7fa866de，VERSION 0.2.3.41）；文档 tip 0bc90cecf（对 4a349ee5 只增 docs／openspec）。集成基点 main 206d41b26dab646c0e50a111eed5a45ccf7c72f6。
现网：GO-R61-PROD-20261119t0921z 第三次尝试（-a3）控制器 SWITCH_PASS 2026-11-19T10:01:37Z（receipt 211be2c7；新 Hub exe a9b3d379，View renderer 49df8fe1）；巡检判据 0e7e7191（review/prodops/r61-prod-closeout-20261119 8af090f1），首轮 PASS 2026-11-19T10:04:54Z。前两次：第一次 STOP_NO_SWITCH（新 release bin 目录模式）、第二次 ROLLED_BACK_STOP（ready 门钉未认证 /mcp＝405，AUTH-FIRST 改 401）；控制器修复 review/opus50tc2k/r61-prod-ctrl-a3-20261119 669b1fa5（togpt60 与 prodops 复核 PASS）。
构建：prodops S-R61-PREP c3440bc1；组长官方工具链 go1.22.2 独立复打 workpulse／fcs／file-service／nodeagent 逐字节同。预发：A 段 PASS 41ae3a99；B 段重做 PASS 02074db6；回退演练 PASS 29ce4b02（review/prodops/r61-staging-a-20261119）。
本文件由组长 opus50tc2k.zzc 于结项时写入。Git 内证据以提交号给出（git cat-file 可取）。

| 项 | 冻结件 | 独立复核 | 组长终审 |
|---|---|---|---|
| r61-n1-busy-wait（N-1a） | 638cb324 | 组长 56 用例 OK、两刀红 | 冻结（N-1b 两项拆出由 r61-n1b-queue-full 覆盖） |
| r61-n1b-queue-full（N-1b） | ed7259fa | 复验 PASS c92efaf2 | 集成第二／三段 |
| r61-n1c-presubmit-wait（N-1c） | 1362f08f（产品 e7d59ab1） | coder56solzzc R6 复验 PASS 5e51a568 | 2026-11-19T04:19:03Z 冻结，第四段 |
| history-provenance（N-3） | 9313197f | qwen38mvb4 PASS 621df13e | 第三段终审 d6e56a8b（2026-11-18T16:27:29Z） |
| mcp-auth-first | 584eb119（产品 ce9b7a95） | qwen38mvb4 PASS 9a2bbb32 | 第五段（令 311288e72426） |
| r61-integration | 产品 4a349ee5／文档 0bc90cec | 作者串行全门 PASS；组长核 merge-tree 0ae99272、测试单独提交、版本位三处 | 2026-11-19T07:24:21Z 冻结 |

已知余项（不阻断结项，另立）：C-6 history provenance 在单 Agent 满额时被拒（S-R61-C6-PROVENANCE-CAP）；控制器入口 --preflight 无参 main() 潜伏缺陷；TOOLCHAIN 记录格式与控制器门不一致（构建侧 r62 统一）。

注：a9b3d379（Hub exe）、49df8fe1（renderer）、211be2c7（控制器 receipt）、0e7e7191（巡检判据）是文件 sha256 前缀，不是 Git 对象；其余提交与 tree 号均已 git cat-file 核到（2026-11-19T10:32Z，0 缺）。
