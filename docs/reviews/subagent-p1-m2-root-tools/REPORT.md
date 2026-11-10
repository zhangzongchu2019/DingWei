# S-SUBAGENT-P1-M2〔51〕-补13：既有主会话工具门补件

CODEX_THREAD_ID=01a1bd1c-c28e-7c60-9594-7894d0078533；to=opus50tc2k.zzc；2026-11-09 UTC。

仓库 git@git.code.tencent.com:wego-ai/dingwei.git；同分支 coder56solzzc/subagent-p1-m2-helper。原交件 d48f3576951a2247233018aab7a091cfc1dfff55 保留；本补件冻结源 e509b44a59b48ffad5fd52e167fe5e23eff8a848，tree 8fc3a383ae3dc5892664e97307f367fad00fab7e。只追加，不改写原提交、报告、原始日志与清单。依据 rulings ac2bc40e0 的〔51〕-补13，交组长独立复核，不自验收。

原M2锁继续由上述线程持有，本线程唯一写者；另一路01a1bacf只读、不碰工作树。不部署、不动现网／预发／在线席位、不启动真实推理。

## 修正

旧主thread没有安装记录时，helper清除待发布的子控制工具定义，上下文明确control不可用，并在节点intent分配之前拒绝创建。错误为 `sub_agent_existing_root_tools_unavailable: 当前主会话未安装子 Agent 控制工具，需重开主会话后再创建子 Agent。`。首次树报告前先核安装记录，旧主不生成树快照；即使残留ready=true，也不能绕过创建门。

原生控制调用也经过同一门。broker只透传helper固定的可公开异常NativeToolUnavailable，测试走真实broker读队列／工具线程／写队列，确认原生调用方收到中文提示；其它内部异常仍用原通用错误。已有主private_endpoint绑定不清空、不调用save_thread替换、不自动thread/start。用户显式重开新的主driver/runtime后，通过原CodexDriver._ensure_thread在thread/start安装工具、保存新主绑定；完成初始树确认即可正常创建孩子。子thread仍不写主绑定。

原M2的29个用例使用已安装工具的主会话夹具；本次明确设置该安装状态。另设ExistingRootToolsTests四例，专门覆盖没有安装记录的旧主，防止将旧主误当成已安装场景。

## 四项验据与四刀

| 项 | 测试 | 断言 | 变异 |
|---|---|---|---|
| 1 不暴露控制工具 | test_old_root_does_not_expose_control_tool | 工具清单没有sub_agent_control、上下文false＋重开提示；强制原生调用也拒绝 | old_root_tool_exposure：恢复清单暴露→OLD_ROOT_EXPOSED_CONTROL_TOOL红 |
| 2 明确错误且无副作用 | test_old_root_create_reports_restart_without_nodes_or_tree | 创建直接返回需重开错误、无节点、report=None、无native thread/start；broker响应也含提示 | old_root_creation_guard：撤创建前工具门→OLD_ROOT_CREATE_NOT_EXPLICITLY_REJECTED红 |
| 3 保留原主绑定 | test_old_root_preserves_private_endpoint_binding | endpoint旧值、driver旧ID、facade旧ID均保留，save_thread未调用、没有thread/start | old_root_binding_replace：注入错误重绑定→OLD_ROOT_PRIVATE_BINDING_REPLACED红 |
| 4 显式重开后恢复 | test_reopened_root_installs_tools_and_can_create | 新主thread/start确有工具参数，树确认后孩子bound；root+child两次start，仅新主保存一次绑定，旧主不变 | reopened_root_tools：撤新主原生工具字段→REOPENED_ROOT_MISSING_NATIVE_TOOLS红 |

四刀均在私有源码副本进行；连同原24刀合计28刀，逐一要求目标断言失败，不把导入或语法错误算目标红。完整方法路径、原文及结果在raw/mutations/。正向基线33项全过。

## 结果

统一以仓根 tests/subagent-p1-m1/run_isolated.py 包装，隔离HOME／短TMPDIR、剥离SH_/DINGWEI_/DW_/DATAD_，普通用户uid1001。命令、UTC起止、退出码与源SHA见raw/*.json。锁定Python为/tmp/coder-0232-locked-venv/bin/python3；OpenSpec1.16.0。

| 门 | 结果 | 原文 |
|---|---|---|
| scripts/run-build-tests.sh | rc0；claude/codex各1162／1162、92文件；27套Python／2套shell | raw/canonical.stdout.gz、canonical.json |
| 新四项＋原M2 | 33／0 | raw/mutations/baseline.stdout |
| M2变异 | 28／28目标红，含新四刀 | raw/mutations/RESULT.json及逐刀原文 |
| r55继承变异 | 21／21目标红 | raw/r55/RESULT.json及逐刀原文 |
| 锁版0.159.2原生探针 | create／来源、伪造参数和旧turn拒绝通过；仅loopback合成SSE | raw/native/RESULT.json、native-tools.json |
| strict | 29／0 | raw/strict.stdout、strict.json |

原生探针二进制SHA仍为1748767b230ebfc3d4ab7e4e254920d0c0ad9691fd8c11f190e7d44511a4a92e，无真实推理。四项补件自身只用临时SQLite、合成原生传输和endpoint夹具；显式重开只发生在夹具中。Go、存储迁移、View/Console均未改。M3仍另派。

SOURCE-SHA256SUMS固定补件与四刀所依赖的源码／规格；ARTIFACT-SHA256SUMS固定本报告、进度与原始证据。原d48f3576交件目录逐字节保留，可在原commit上复算其原清单；本补件使用独立清单复核当前源。
