# R60 正式集成候选交审

固定产品/测试源 a311aaf3beea90262c05ca2c316064b8a8856d06，tree dfb32347b5156dc1d15d4bfaf0543310dde316d8，父 6f7dcad70d1b3aa308f38a73432dce6c8397f045；基110e2c1f，VERSION0.2.3.40。按③④①②合入及冲突解法见CONFLICT-RESOLUTION.md；本报告是作者交审证据，不自签收、不是发布GO，不推main/不接现网。

## 结果

完整固定源门达到作者交审条件：build.sh内部执行完整run-build-tests两profile及四个二进制构建、其他三包树与m8五轮PGstrict、focused race、浏览器全量、OpenSpec strict均按各自原始记录保留。具体计数从evidence/gate-summary.json与原日志取，不把组件/评审旧门合并成一次全绿。

| m8轮次 | rc | 顶层PASS | 含子项PASS | SKIP | FAIL | interrupted行数 |
|---|---:|---:|---:|---:|---:|---:|
| m8-1 | 0 | 970 | 1637 | 29 | 0 | 0 |
| m8-2 | 1 | 969 | 1636 | 29 | 1 | 1 |
| m8-3 | 0 | 970 | 1637 | 29 | 0 | 0 |
| m8-4 | 0 | 970 | 1637 | 29 | 0 | 0 |
| m8-5 | 0 | 970 | 1637 | 29 | 0 | 0 |

Z3按组长2026-11-17T01:37:18Z门，至少三轮完整全绿；逐次逐名interrupt和FAIL在gate-summary各runs中完整列出。只有两个指定格可作例外，不将FAIL改SKIP，不用单跑盖掉整包。根因另属r61，未实施store/驱动修法。具名跳过见SKIPS.md，真实CLI与联合格解释器已显式指定，不靠SKIP过门。

## 门读数与范围

- 四树Go的代表绿轮m8与其他包分段：顶层1601 PASS、含子项2670 PASS、41具名SKIP、0FAIL。此处取第1轮m8和另外三树，各自命令/窗口单列，不伪称一次单进程四树绿；第2轮已知失败仍在上表。
- SOURCE_COMMIT build.sh：rc0；内部完整canonical Claude/Codex各1270、inventory97、Python27/shell2，末行BUILD_TESTS_OK；随后四个二进制构建成功，源码锚与SHA在canonical-build.log。不上传本地二进制。
- focused race：32顶层/41含子项PASS、0SKIP、0DATA RACE；不是整包race。版本钉住另外3项PASS，未混计入四树Go。
- 浏览器全量runner：45/45套件rc0、aggregate_rc0，包括renderer_agent_icon_admission；报告覆盖当前a311源，非旧UI证据复用。C2套件原样报告既有T-166 legacy控制提示KNOWN-DEFECT，保留原文；套件rc0不等于这些既有断言无缺陷。不为本单扩大UI范围。
- OpenSpec1.16.0全仓strict：40 passed/0 failed。固定源85项产品manifest逐文件SHA与Git对象一致。

## 新增保护8刀

四处Z1防线、heartbeat current、重复CLI恢复原回执身份、联合两秒截止、联合重试原source共8刀，均编译成功且对应业务断言红；还原后Go与心跳基线绿。before/after、精确命令/UTC/编译与业务rc、断言、原SHA见mutations.json及逐刀日志。与组件①55刀/②28刀/qwen独立刀分开，不相加声称新覆盖。

联合source变异实际造成Hub created=2，被原source/delivery与单责任断言抓到。真实helper自动writer在原10秒回执等待后恢复，dw_send_status只观察；本源不启动模型，不把一次Hub创建冒称网络只送一帧。Z1队列格刻意不启动显示worker，防止独立清理掩盖防线，属于受控队列加真实状态WS测试。

## 历史与复核

WIP首次多余import编译红、第二次子进程结束标记缺失红，第三轮修夹具后绿；原始wip-focused日志全保留，均不是最终源门。首个变异driver在offline_stop匹配到两处而停止，未执行该刀，不计有效变异；缩窄锚后继续，其余有效刀/还原记录可复核。缺openspec可执行名的首轮用绝对锁定1.16.0入口修正，最终strict另记。

qwen①报告aa41初次遗漏10个.gitignore日志，65713e648补齐后按Git对象12项SHA全中，verification单列；不是本席重放qwen门。①qwen报告review/qwen38mvb4/r60-send-lost-20261117@65713e6483672cdf75739537bd69e78fcd9fedcc（tree45e45e17cfcfc4e1c49296d1c2ac51f5881bb16e），②qwen报告review/qwen38mvb4/r60-hub-keepalive-20261116@adcb1d649e8f709f030f9e2c603f99a6427a4785（tree219ecb9b7ae2bb0b0cfbeab2f16d05cb0e6280f8），均已独立fetch。组件作者、组长与qwen的按锚PASS保留，不等于本集成独立PASS。Z2全局obs差值只依实际门结果处理，没有预改夹具。

## 发版可见面与残余

send.py JSON/退出码0/2/1、原ID查询/恢复及上线后板件引用更新清单见SEND-CALLER-CONTRACT.md。物理磁盘无硬上界、窗外未终结caller/outbox/Hub栅栏增长、caller/outbox行数观测、helper分块吞吐代价与未知回执恢复边界见RELEASE-NOTES-DRAFT.md。未知结果不能换新ID重跑正文。保留现网renderer retainCanonicalTrustedProvenance与对应浏览器回归；VRW-PM01仍关闭copy重发。

本次仅构建本地隔离验证件，不上传二进制、不申请上线；预发/现网上线时机与回退执行依组长/业主另令。
