# B 同 R1 第二次返修：节点最终 gate 回执

仓库 git@git.code.tencent.com:wego-ai/dingwei.git；分支 review/coder/seat-capacity。
正式窄 REWORK `283d3da7323dfd8f6ccd5c951b2a0920b4cdec0d`，前件 `695bec8bd2a46d2e01913d83f6d1ed687d1e6697`。冻结代码 `7d5cba6039a7cd739fa13c7c16d52a6d8c88caaa` / tree `3ad51486db473000f1ec3df86e4b44852ae9f664`。只补节点最终合法容量拒绝链；仍基 r22，未重基/合入 r23。本文是作者证据，待 opus50ts 精确复审。

## 识别与投影合同

Store.capacityResult 只读取已登记节点的受信 Gateway 响应（调用者持有请求对应的 nodeID）。结果必须 OK=false，Error 全长≤160；固定首部必须从第0字符匹配 `seat_capacity_blocked: seat_capacity_`；其后按 + 分隔，1..6个、无重复的完整 token，只允许 cpu/memory/disk/seat_limit/load_unknown/policy_invalid。空项、任意前后文、换行/空白、重复、未知 token、过长输入均不识别，不 substring 搜 cpu，不提取节点原文。

还须注册模式为显式 soft、合法 policy revision，已存 frame 的 quota_mode/revision 与注册绑定，并有 seat_capacity.version=1。hard/缺B/版本或revision不符不解释为 B。特意不以 Hub 的 Known/fresh 再判该固定回执：该路径正是为心跳与节点最后采样不同步，可信节点已最终拒绝。协议识别不代替原 Gateway 的认证/命令关联，本次**没有实际 WS 认证握手或 Docker 资源耗尽验证**。

识别后复用前件 typed error / Unwrap / allowlist：policy_invalid 折 load_unknown，不回显路径、节点ID、数值、模型名或原文；旧机器码保持 node_no_capacity，HTTP/job 另带安全 capacity_reasons。真正 soft_quota_blocked、普通 Engine/transport 失败继续原分支，不重试。

## 真实执行入口

- Lifecycle Upgrade/Restart/RefreshCred/Enable：在正常 Gateway 回执、原 hold/普通失败分支之前识别严格 B 回执。容量拒绝不伪报 start_failed 或 hold；条件恢复该操作仍持有的 busy/starting 状态及原 detail，保留原凭据 Resume 的撤销围栏，不覆盖已经连回的状态。返回 typed error，经既有 durable lifecycle reason 写读投影。
- Upgrade/refresh 原本在发命令前的 token 签发/账号解密准备仍会发生；本件不声称把这些准备挪到节点 gate 后，也不声称真实 Engine 上未发生任何准备。升级未领取 token 的失效/旧凭据保护沿原机制，无新增重试或签发动作。
- Orchestrator RunJob 的 pull_image/create_seat 最终 gate：合法 B 回执走原新建 fail 清理，drop 暂存 token、解绑账号、AbortHostedAgent；持久 reason 只存固定 token。不复活已吊销身份，不把新建容量拒绝伪报镜像失败或 soft hold。其他 create 失败的原 soft 保留分支不改。
- 创建 jobViewOf 与生命周期 job 共用 capacityJobView，owner 读面投影为 failed/node_no_capacity + allowlisted reasons；其他旧 reason 仍按旧列表处理，权限/状态机不改，没有 schema 变化。
- Console 创建蒙版与反馈、既有操作轮询均用固定资源文案。原非容量创建失败文案保持原样；不显示 raw Error 或任意 message。

## 覆盖与范围

新增3个 hosted 顶层：严格协议/注册绑定，真实 Store+Enqueue/RunJob/JobProgress 的四种生命周期操作×CPU/内存/盘/unknown，真实 Create/RunJob/JobProgress 的 pull/create×四维。Hub frame 始终 fresh-good，仅伪 Gateway 在最终命令边界返回 nodeagent 的固定格式；这是边界注入，不是实际资源漂移/认证网络/Engine 测试。

正格确认维度、durable owner读面、非owner拒绝、既有席状态/AccountRef和暂停状态恢复，新建原Abort/解绑/token清除，以及每条命令只发一次。负格包含近似/污染/过长/重复/未知串、OK回执、hard、无B/模式/revision/版本不符；实际 restart job 的 ordinary Engine/transport/真hold 各保留旧码，原文不泄漏。

新增实际 workpulse HTTP 测试：POST后让后台RunJob在 pull/create 最终gate拒绝，等待原 abort completion channel 后 GET真实job；两维仍可见、非owner403、Abort/解绑完成。不是用伪JSON冒充后端PG路径。浏览器另走实际 hostedTrackJob→loopback job→创建蒙版/反馈，核CPU/内存与恶意字段不显示；保留前件21基线、7原因与操作job断言。

计量、nodeagent、纯计算器、hard范围、VERSION与runtime锁字节不变。原已闭合Hub路径不重做；为本次改动运行相关生命周期/真实hold/普通失败回归及必要canonical，不重复未变nodeagent/计量全包。不把r23旧组成证据重标为本次采用合并验收。

## 精确结果

- 精确7d5cba6 定向race：hosted26＋workpulse4顶层、无fail/skip，rc0，123.348s；涵盖上列新测试与旧生命周期顺序/并发/超时/晚连回/凭据轮换/真hold/普通失败。是相关定向命令，不冒称两个包全量。
- 同钉完整canonical rc0，125.773s，python48＋shell2，含临时PG parent与五个claim子格。
- 同钉浏览器rc0，3.726s；全部撤修后恢复再跑rc0，3.866s。
- 八刀 source withdrawal 均实际断言红：识别总入口、严格首部、注册/B绑定、restart回执分支、pull投影、create投影、创建job读面、创建Console显示。候选源码未改，变异只在私有副本；恢复4个目标顶层race rc0，10.853s。
- 严格首部初轮脚本误用三返回值 strings.Cut，导致编译失败，**不计有效撤修**。保留 initial 日志/metadata；仅改负格副本为 TrimPrefix 允许错误前文，实际触发 near-match accepted 断言红。最终八刀聚合和每刀SHA/命令见 raw/reply-withdrawals.json、reply-mutations.json；原执行脚本和单刀纠正脚本都按实际保留，没有冒称首轮全绿。

前件695已闭合项与前件/原件canonical/nodeagent结果仍按各自源码归属；本目录仅封签本次增量。源码交换仅Git；无宿主、真实Docker、外部PG、密钥或真实Agent操作、通知/制品构建/部署。四席冻结与owner后续执行门保持不变。

原审查fixture原字节的作者复跑：283d3da7所钉 final-node-gate_test.go.fixture 复制到私有源码副本，两个顶层（restart及pull/create两子格）race rc0，11.065s。fixture不可变坐标及SHA见SOURCE.json；这是作者复跑审查探针，不能标为审查方对新件的独立PASS。

收尾：移除本席私有变异/探针/cache/HOME/TMP共 1239119636 bytes；Go cache先clean至 232 bytes再删除。所有owned进程/PG marker为0，剩余临时路径0；源工作树留审，未动共享module cache/锁定venv，详见raw/reply-cleanup.json。
