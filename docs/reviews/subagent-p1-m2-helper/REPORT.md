# S-SUBAGENT-P1-M2 helper 交审报告

CODEX_THREAD_ID=01a1bd1c-c28e-7c60-9594-7894d0078533；to=opus50tc2k.zzc；2026-11-09 UTC。

仓库 `git@git.code.tencent.com:wego-ai/dingwei.git`，分支 `coder56solzzc/subagent-p1-m2-helper`。基线 `eae9eca1ac67bf580f4b0f8dccca5f92d7e62a88`。最终候选／测试冻结源 `826a7c0ecf3d3b5634f69eed479d6c5abdf3b119`，tree `8742f8df5e30736372eb8525315a05ecd9d96901`；产品冻结源 `eed6ac54762bb2cf756f78934fbfb2db46f73d64`；已推送并核远端。证据提交只追加报告、进度和原始记录，不改冻结源。走 r57，不并 r56；独立验收由组长完成，本席不自验收。

写者锁 `/tmp/coder56-subagent-m2.lock` 由上述线程原子取得，本线程是唯一写者；另一路 01a1bacf 只读、不碰工作树。M1 锁已获准释放；M2 锁交审期间保留。

## 改动与边界

单 app-server broker 管唯一传输、ID映射、初始化、通知分派和有界公平写队列，物理写与响应等待分开；server request 即使同响应 ID 相撞也不会串线。未知／缺 threadId 只作有界隔离，不猜主席。各分道独立 DriverAdapter/FIFO/CodexDriver，共用冻结 r55 对账实现和席级限额。

helper SQLite 加法表／列保存树、别名／墓碑、创建 intent/submitted、五项策略、逐条退信、原生工具发信幂等和投递作用域；旧数据默认主席作用域，旧读数保持。每个单查／更新／回调限制 lane_id/generation，原 inbox 主键通过不可见的内部存储命名空间保留旧结构。新节点必须得到当前连接完整树 ACK 才可接信。

创建请求幂等且先落 submitted 再创建 native thread；丢响应或重启不创建第二条、不从 thread/list 猜绑。原生创建等待移出管理锁，父结束仍能立即封口；迟到创建结果只登记 closed 绑定与墓碑，不复活。默认名取 UUID 随机尾8位，冲突逐步加长；墓碑别名不复用。

生命周期解析默认：完成即退开、idle1h、max2d、每节点 cap5d、grace10min、父结束级联。先事务 draining，再逐条确定未执行退回；可能已写出的保留 unknown，不伪造 processed/ACK。idle 在 adapter 恢复前也查持久工作；绝对期限跨重启不清零。关闭单分道不杀共享进程；真实物理失活或写阻塞由席级 broker 恢复。

发信只能经 native dynamicTools 外层 threadId/turnId 与持久节点代次绑定。arguments 不接受来源字段，IPC/env 无子来源选择入口。原生调用在发送前持久去重；崩溃后 unknown 不重发。裸目标全名只补既有 #key 传输封装，保留完整子路径。M1 的 delivery_ack_confirm 不回显节点字段，因此接受投递同事务保存不可变 scope sidecar；确认先唯一定位该侧表，再进入 scoped inbox，歧义不选兄弟，不在回执账本 LIMIT 1 猜归属。

默认开关 `SH_SUB_AGENTS_V1=0`，仅显式开启的已入网 Codex driver 宣告能力。启用前需正确的两段主全名配置；本单未更改任何席位配置。

## 核验结果

所有命令在仓根、普通用户 uid1001，以 `tests/subagent-p1-m1/run_isolated.py` 包装，隔离 HOME／短 TMPDIR，剥离 SH_/DINGWEI_/DW_/DATAD_；锁定 Python `/tmp/coder-0232-locked-venv/bin/python3`、OpenSpec1.16.0。每个命令、开始／结束UTC、退出码与源 SHA 在 raw/*.json。

| 门 | 结果 | 原文 |
|---|---|---|
| canonical `sh scripts/run-build-tests.sh` | rc0；claude/codex各1158／1158、92文件；27套Python／2套shell | raw/canonical.stdout.gz、canonical.json |
| M2 专项正负例 | 冻结源基线29／0；两profile内再执行 | raw/final-special-29.stdout、mutations/baseline.stdout |
| M2 每格变异 | 24／24目标红，runner rc0 | raw/mutations/RESULT.json及各刀原文 |
| r55 继承护栏变异 | 21／21目标红，runner rc0 | raw/r55-mutations/RESULT.json及各刀原文 |
| r55 九格矩阵 | 9格，Hub rc0；原有控制逐字段不变、混精度仍pending | raw/matrix/RESULT.json、CONTROL-COMPARISON.json、matrix.json、hub.stdout |
| 锁版原生工具 | 主工具创建孩子、两个真实子thread工具调用、outer来源、伪造arguments与旧turn拒绝，全过 | raw/native/RESULT.json、native-tools.json、native.stdout |
| m8 全包 | 1524／0／37 SKIP | raw/m8.stdout.gz、M8-SUMMARY.json、M8-SKIP-REASONS.json |
| OpenSpec `validate --all --strict` | 29／0 | raw/strict.stdout、strict.json |
| 兼容 future | rc0，两条目标保护绿 | raw/future.stdout、future/RESULT.json及Go原文 |

m8 在中间冻结源 f2e2c37a5 上执行；截至最终 eed6ac547 所有 Go 源、go.mod/go.sum 逐字节未变，见 raw/GO-SOURCE-UNCHANGED.json。6d18bc29b仅更改新增测试隔离及变异错误过滤，产品与原生/r55探针输入全部与eed6ac547逐字节一致，见raw/PRODUCT-SOURCE-UNCHANGED.json；对应原生、九格、继承变异读数复用该冻结产品验证，M2的24刀在d9f05218c上执行，826a7c0ec仅改变deploy库存断言、对应输入逐字节未变；canonical以最终候选重跑；d9f05218c追加turn/completed不结束任务的显式断言及对应变异；826a7c0ec同步新增测试后的精确库存数92，产品仍未改。37个可选 fixture 的具体名字和条件逐项记录；不把 skip 算通过。canonical 的库存与其必跑集另以最终原文为准。

原生二进制 0.159.2 SHA256=`1748767b230ebfc3d4ab7e4e254920d0c0ad9691fd8c11f190e7d44511a4a92e`。原生探针是独立 app-server＋本地合成 Responses SSE，无真实模型推理、无凭据、无在线 Hub；发信回调合成，实际 WS 线协议和持久确认由独立 helper 正负例验证。未把该实验称作真实在线子会话发信或 M3 混部矩阵。

## 判据对应

| 判据 | 正例与负例 | 撤保护的刀 |
|---|---|---|
| AA004 来源 | 原生外层 thread/turn 到节点代次；arguments来源、旧turn、关闭节点拒绝；发信调用持久去重；完整目标＋durable receipt | outer_source、stale_turn、argument_source、send_journal、wire_destination |
| AA006 命名／告知 | 同前缀UUID随机尾段、尾碰撞加长、墓碑别名禁复用；父／孩子／策略／全名上下文 | timestamp_prefix、tail_collision、self_context |
| AA007 生命周期 | 五项默认／上下界、显式完成与禁用、父级联、restart unknown不idle、cap／grace、逐封退信、创建等待不挡封口 | default_completion、unexecuted_filter、platform_cap、parent_cascade、completion_override、persisted_idle、creation_lock、turn_is_not_task_completion |
| AA008 broker／注册 | thread唯一归属、未知事件隔离、双向id相撞、双lane并行、关闭A不伤B、writer检查、ACK门和恢复后事件泵 | event_owner、request_shape、child_stops_process、ack_gate |
| AA009 账本 | 同封兄弟、root/child、旧代次、回调／单查限制、返回结果分道、M1无lane回显的确认持久定位与歧义拒绝、席级预算 | envelope_unscoped、shared_inbox、callback_lane、confirmation_scope |

29个专项方法名与每个变异准确对应测试在原始 RESULT.json；变异仅改一次性私有副本，必须以指定断言失败、不能把导入／语法错误算目标红。正向全组先过，原工作树无变异残留。

## 首跑与限制

开发期的fixture构造参数、缺pytest、早期变异目标定位等失败也原样保留在raw/development；不计为目标红或门绿。首轮工作中 canonical 暴露旧 AST 替身缺新增可选方法，及源文件尚在修改时 inspect 行号失配；随后按默认关闭路径隔离可选调用。候选阶段的新增 stdio 碰撞用例还暴露库存 fixture 重载 codexDriver 后，发现期旧类残留另一个 fixture 的命令补丁；运行时取当前类仍会遭遇旧补丁（candidate-eed），最终在干净子进程运行实际stdio实验，PYTHONPATH固定当前被测源码，含变异副本；专门将父进程构造器破坏后该用例仍过，见inherited-fixture-isolation。初始／候选失败原文全部保留为 initial-canonical、candidate-f2-canonical、candidate-44-canonical、candidate-eed-canonical。

原生创建阻塞封口的真实反例先红在 NATIVE_CREATE_BLOCKED_LIFECYCLE_SEAL；修复移出管理锁并新增逐刀，保留 lifecycle-counterexample 与 lifecycle-fix-positive。candidate-6d的双profile各1158/1158已绿，总门后续因入口固定库存91而红；826a7c0ec将精确库存更新至92，逐项删除文件/条目负测保持，独立17例通过后重跑总门。M1确认正例第一次漏了 type 字段，属于新fixture不完整，补齐与 M1 完全相同形状后通过，首跑也留存。额外旧 r55 mutation runner 有一个已不存在的旧 selector，不把 AttributeError 算变异；新 wrapper 只将该刀映射到已批准的混精度保护及当前用例，其余20刀原封复用，最终21刀均目标红。

0.159.2 的 thread/start 支持 dynamicTools，thread/resume 没有补装字段。历史主 thread 若最初未安装工具，明确记录并报告 `sub_agent_existing_root_tools_unavailable`；不自动替换主绑定，不假称升级后可以调用控制工具。新孩子在创建时装工具，来源已实测；历史主 thread 的控制工具入口需运营方显式建立带工具的新主 thread，本单不执行该迁移。中间回执沿用的 strict27数纠正为最终原文29／0。

M3 的 View/Console、混部与真实回退演练仍另派。回退原则在 docs/design/SUBAGENT-P1-M2-HELPER.md：先封口、逐条退信／保留unknown、ended快照ACK，再停协议；保留全部加法表与墓碑，不 DROP，不把孩子导回主席。

本单未部署、未触现网／预发／在线席位，未启动真实推理；源代码只经私有 Git 交件。SOURCE-SHA256SUMS核19件冻结产品／测试／设计／库存；ARTIFACT-SHA256SUMS核报告、进度及原始证据。可分别在仓根 `sha256sum -c` 复算。
