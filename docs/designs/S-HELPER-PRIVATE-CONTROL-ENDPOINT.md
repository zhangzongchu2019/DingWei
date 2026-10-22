# S-HELPER-PRIVATE-CONTROL-ENDPOINT（一页设计，待审）

基已审 `ce58c1bf`（完整锚见附证）；分支 `review/coder/private-control-endpoint`。r37 `258267c7`在组长集成门中，实施基线待其正式锚对齐；本件仅docs、不改席位。

**路径与开关。** 拟加启动配置 `SH_CODEX_ENDPOINT_MODE=legacy|private`，默认legacy，原身份门默认observe不变；private须本组逐席另批并显式设身份门enforce，组合不符拒启动，不暗改默认。私有根取该席已核 `cfg.state_dir/codex-control/<seat及原CODEX_HOME的摘要>/`，逐级防软链、核本UID和非组／他写，叶0700、文件0600，Unix路径超限即停，不回落共享/tmp。保留原HOME／CODEX_HOME与配置、凭据、rollout、driver账本，不复制／分叉登录态；同UID恶意代码可读写该UID文件，不声称0700构成同UID安全隔离。

**自持句柄与单实例。** 独立小型endpoint-owner（helper包内入口）持有 `flock(LOCK_EX|LOCK_NB)`、Codex Popen与启动后读得的PID＋starttime，启动核定原生Codex `app-server --listen unix://<私有根>/app.sock`；不用daemon start、shell或“最新PID”。helper同owner的短本地控制通道限status／quiesce／stop，定长、超时，无通用执行能力；注册helper唯一写租约，重复helper不得同时准入新投递；owner原子写无凭据registry（格式／boot ID、owner及子进程身份、根／CODEX_HOME摘要）。新helper只在私有路径、registry属主模式／版本、控制FD的SO_PEERCRED＋owner PID/starttime／核定入口身份与本次challenge均匹配时复用；registry本身不授予接管或杀进程权。活owner从仍持有的Popen核子进程存活，提供owned绑定；helper对app.sock同FD强比UID／exe／PID／starttime，连接代次改变必重核，任何不符不发HTTP／thread／turn、不退回legacy。

| 生命周期 | 明确行为 |
| --- | --- |
| 首启／双启 | 先锁后监听／启动；竞争者核活owner后复用，矛盾、半写或未知socket即失败，不覆盖路径 |
| helper停止／升级 | 只断开helper客户端，owner与Codex保持；固定owner运行包在owner退出前保留，升级清理不得删；新helper按版本握手复用，不凭PID文件领养 |
| Codex异常退出 | owner由wait确认并回收，停新投递；只移除自己登记且inode仍匹配的socket。最多3次有退避重启，再对旧thread读／resume；active／unknown投递不自动重放，失败session_unavailable |
| owner失联／崩溃 | 新helper拒接遗留app.sock，不补启第二实例、不杀疑似孤儿；留证，由本组获批恢复单核遗留后再启。明确不是无损自动恢复 |
| 显式停owner／回滚 | 冻结新投递，确认无活动turn、无人工接入；owner仅停止自己仍持有的子进程并wait，精确清自己对象后释放锁。无法证明空闲则拒停，不强杀未知进程 |

**人类与恢复兼容。** 人类用受控接入入口先核活owner，显式 `codex --remote unix://PATH resume <已绑定ID>`；锁定两版help均支持remote／resume，proxy另有 `--sock PATH`，不改全局Codex配置、不把默认agents／proxy误接当成功。入口记录有期限的接入lease，失去lease心跳列为未知接入而非擅自判空闲；直接绕入口的连接无法完全识别，显式停owner还须操作人确认；不做透明热切。只凭help不宣称交互已验：实现门须真实PTY证明人工接入／退出、helper仍同一peer及thread、通知不串绑、已有活turn不被重开。恢复沿用已审thread/read→必要时resume及cwd／canAcceptDirectInput重核；missing／鉴权拒绝保留账本并停，不换thread。正常升级可复用owner；维护重启Codex仅在空闲窗口恢复同一持久ID，不把共享CODEX_HOME说成活会话兼容已通过。

**验收与发布。** 0.161.0／0.163.0私有HOME、同CODEX_HOME隔离验证：双启只一子进程；错owner／exe／PID重用／软链／模式／路径过长拒绝；helper重启owner不变；child退出恢复同ID与owner死遗留拒接；人工PTY正反格、busy不停止、账本不重投、锁及精确清理；逐门指定断言撤修红。全套helper（隔离HOME锁定venv）、canonical、全仓Go／vet、相关事件浏览器及版本兼容门。仅本组先验证；未知协议不猜，实测不支持则设计返修。迁移先停写／人工退出、核旧绑定、启private并验证后才恢复投递；回滚经owner空闲停机、恢复legacy配置、显式旧thread核验，不重置凭据或账本，不影响组外。实现、升级与上线均待后续令。

依据：[锁定版help与前件隔离实测](../reviews/coder-private-control-endpoint-design-20261022/REVIEW-INPUTS.md)；[OpenAI CLI参考](https://learn.chatgpt.com/docs/cli/reference)仅作背景，不替代锁定版实测。
