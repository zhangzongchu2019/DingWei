# 0077回退只读取证

依据组长2026-11-17T03:19:05Z令。所有实测在隔离临时库/loopback完成；不改产品、不接预发/现网、无真实模型/席位/PG。旧exe由110e2c1f同源源码本地go build诊断编译，版本日志0.2.3.39；不是拉取现网exe，不声称其字节等同线上制品，也不分发该二进制。新exe为此前固定a311 SOURCE_COMMIT build.sh本地产物。110与main d2的internal/cmd/VERSION/go.mod/go.sum差异为空。

binary-roundtrip.json及三个进程原日志：新建库施加0077、旧件在原库启动/停、再前滚同库启动/停。三个health200/退出0；0077账本、两显式索引及三条W/P/E栅栏逐项相同。

target-final-probe.json：相同Go探针分别叠到旧110和新a311独立工作树（只加临时_test.go，不入产品），复制真实施加0077的隔离库；使用MemStore身份和真实Hub/WS。已入队正文绑定原Target，撤原席并建立同名新席且加入合法共同组。旧件同名新席实际收到正文state=sent；新件未收到、state=dead/outbound_permission_denied。最后实际SELECT核原fence/target仍一条。早期target-probe/old-target日志保留，其fence=true仅为打印标签；报告只以补实际SELECT后的final探针作为fence实测依据。capability-probe分别通过真实WS测旧recovery=false/新true，旧durable_agent_queue=true不是新幂等能力。

helper-old-peer.log是固定a311新helper两条既有定向单测：旧/缺能力不发正文；曾unknown见unsupported后，即使再升级也不自动重放。它们用受控Broker，不冒称已做真实旧Hub+完整helper组合实跑。

forward-gc-final.json/log：从停止的隔离库用SQLite backup API复制，重设合成W/P/E各1；在新store真实Commit API先恢复W/P原delivery，再受理一个新ID，触发真实GC。W/P/E=1/1/1→2/1/0，旧W和P同ID均created=false，E清、P留；不是手写DELETE模拟GC。P为专门构造的超期fence/未终结delivery，用于GC边界，不推导生产超期分布。早期forward-gc.log同结论保留，最终取带argv/UTC/SHA的final记录。

不归档私有SQLite/任何二进制/凭据/完整真实信封。所有ID/正文为测试合成；probe源文本只进私有Git，未走文件服务。脚本绝对路径须在独立工作树按隔离缓存调整；不是预发/现网操作脚本。SHA在上级SHA256SUMS统一核验。
