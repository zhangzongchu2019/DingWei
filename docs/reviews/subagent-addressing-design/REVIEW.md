# S-SUBAGENT-ADDRESSING-DESIGN 交审证据

01a1bacf-9317-7b91-bf86-0a0c88c834f6；to=opus50tc2k.zzc；2026-11-08 UTC。

仓库 git@git.code.tencent.com:wego-ai/dingwei.git；分支 coder56solzzc/subagent-addressing-design。基线6e2c836491f8ba506833d6f849fbe73530e13f92；输入b33f9ae6108e1e86778aee5e4040d3847f06dce8；r55整合前置3dc5cd77f683bd21653955c76f97647ce78034f3（本分支不改产品代码）。最终commit/tree由交件回执给出，不在正文自指。

## 读数与命令

所有命令工作目录为仓根，普通用户uid1001。精确日志最后写入UTC见raw/RUNS.json；Go原始日志时区未改写，不能把其本地时间字段当UTC。原始文件原样保留，SHA256SUMS校验raw所有文件，另附ARTIFACT-SHA256SUMS核设计/规格/复现脚本。

| 命令 | 真实退出码 | 原文 |
|---|---:|---|
| `PATH=/tmp/coder-0232-locked-venv/bin:$PATH python3 -B tests/subagent-addressing-design/run_legacy_probe.py --output /tmp/subagent-design-legacy-delivery` | 0 | raw/legacy-go.log、legacy-RESULT.json、legacy-run.stdout.gz |
| 同上加 `--future --output /tmp/subagent-design-future-delivery` | 1（预期目标红） | raw/future-red-go.log、future-red-RESULT.json、future-red-run.stdout.gz |
| `python3 -B tests/subagent-addressing-design/native_parallel.py --binary /tmp/coder-stale-protocol-01592/codex --output /tmp/subagent-design-native2` | 0 | raw/native-rpc.json、native-schema.json、native-RESULT.json、native-run.stdout |
| `OPENSPEC_TELEMETRY=0 /tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec validate sub-agent-addressing --strict` | 0 | raw/openspec.stdout |
| `PATH=/tmp/coder-0232-locked-venv/bin:$PATH sh scripts/run-build-tests.sh` | 0 | raw/build.stdout.gz（gzip保留完整原文） |

legacy观察7个Go控制均过；Python三个入口只调用纯函数，未发送IPC。完整三段目标走实际RouteEnvelope返回“组内找不到该Agent”，父WS350ms观测窗口无原信；这是与拒收错误一起的有限窗口实测，不将超时单独当无限期不存在证明。parseAddress接受含点会话名只是传输拆分。Console拒绝三段订阅，View后端单取header不自行校验身份，send.py只补井号。旧在线清单实测接受三段字符串，且其中root后缀也被Contains误判存活；它不具有路由或树权威。

future仅两个失败，且红在目标断言：AA005_CHILD_MENTION_NOT_SKIPPED_WITH_VALID_ROOT_RETAINED、UNNEGOTIATED_SUBPATH_REACHED_PARENT；其他五控制保持。后者实际走隔离Hub/WS，输入父地址+未知sub_path，父确实收到；未声称完整子全名会误投。这两刀是基线缺保护复现，不冒充实施完成后的变异验证；后续每条保护的变异计划见设计§8。

native `--version` 原文见raw/native-version.stdout；硬钉0.159.2，SHA256=1748767b230ebfc3d4ab7e4e254920d0c0ad9691fd8c11f190e7d44511a4a92e。独立app-server、两个真实thread/turn，两次loopback SSE请求并行，两turn同时inProgress；中断/退订A后B仍inProgress，再清理B。实验schema从此二进制 `generate-json-schema --experimental` 导出，含thread/start.dynamicTools与DynamicToolCallParams。没有真实推理、没有native spawn-agent、没有端到端子发信工具来源验证；这些是明确后续实施门，不伪报已过。未上传二进制、凭据或在线私有库。

canonical 两profile各1097/1097（基线90文件），27套Python与2套shell均过，末行BUILD_TESTS_OK。本分支未移植r55，故库存读数不是r55的1121。

## 规格及证据边界

保留b33提案原文，delta补场景与最新结束态口径，新增AA007～010待复核。初次strict因原delta末尾额外“第一期范围”节无法archive而警告（非功能失败）；已归入AA010及design，最终strict0。首轮overlay因vet无法打开虚拟新文件而未编译；runner仅其隔离命令用-vet=off后重跑，最终记录基于成功编译版本，不把基础设施失败冒充目标红。native首次非experimental schema缺少dynamicTools；最终改experimental重跑，证据采用native2完整输出。

扫描722个tracked源文件、结构候选123行，原文raw/source-inventory.txt；人工追踪调用点覆盖设计§7，主身份/管理保持与明确不支持均列明。排除的host/JWT/version点分割、测试/历史证据不是地址解析。复现文件放tests/subagent-addressing-design，不进入helper默认90文件清单，不修改canonical测试列表/产品代码。交审不自验收；组长独立复核后再决定实现与默认策略。

提交前同步核对：远端r54基线6e2c8364与提案b33f9ae6均未变化；rulings从6ec4d930推进至5c142200ea7066b36fc8889849126a61eb82aeb4，新增仅〔52〕补7/补8，不影响本设计。
