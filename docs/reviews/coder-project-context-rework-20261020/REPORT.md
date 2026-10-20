# 第85条 project_context 窄返修

基 review/coder/pilot-project-context-impl 的 9dc27ecbee668dac9706325e0e52b95fe8fba20b；同分支单个后继，不改写原交件。运行输入冻结 Git tree f5fe34aa5b107e9f510272a8057a0a34ceecf1f1（不是 commit）。REPRO-GATES.py 为文档目录中的复现工具，不改变被测生产或单测字节。组长审、prodops 交叉核；无部署/席位/真实服务操作。

## 闭合项

1. TestProjectContextArchivedSpecAndUnreconciledPeer：实际归档 spec 后请求 all 无该任务；归档过滤撤掉命中 PC_ARCHIVED_SPEC_LEAK。正对照先证明任务可读。
2. 同一HTTP测试真实建立以开发 Agent 为组长的新组，改派后移除对方审查 Agent，不运行reconciliation，断言 NeedsReassignment 仍 false、当前开发 Agent 读取无任务。身份层另有 TestProjectContextClaimMembershipAndOwnerEntitlement：对方移组、owner非创建者且无当前 active 成员分别拒绝；恢复成员和创建者均有正例。撤掉对方成员检查、放开 owner 成员归属，各命中指定红。
3. helper 仅映射已知HTTP公共错误码，不反射服务器正文。409返回context_changed及discard_cursor=true；400返回invalid_context_request及discard_cursor=true；503返回project_context_unavailable且retryable=true。不自动重试，不返回旧游标。12项Python模块测试中有真实本地HTTP三子例，分别验证错误码、重试属性和游标处置；撤修各指定红。
4. 实现文档补齐：任一所需 PG 行缺失整请求503；Fingerprint每页重扫候选任务；单条超256KiB永久413，缩limit无效，必须缩小内容。产品权限过滤保留。

## 证据与限域

withdrawals/RESULTS.json 六项全部 designated_red，baseline/restored Go与Python均rc0；每项原始输出保留。gates 为不可变 Git tree 导出、私有HOME、离线依赖：context三包定向Go、vet、race、canonical。最终rc以各JSON为准；原候选全仓52包与helper756项保留为前序，本窄返修未重跑全仓/完整helper，不冒称新全仓通过。新增helper模块为12测试。

开发初跑夹具错误：原r32组长是审查Agent，不能直接移除；首跑返回identity: group leader cannot be removed。已改为通过生产CreateGroup/JoinGroup/AssignConsoleTaskFlow创建开发为组长的夹具再移除审查；没有放宽组长保护或产品权限。

VERIFY.py 从候选Git blob核验源码及本目录证据；source_commit 字段指上述冻结tree。当前会话VM-4-12-ubuntu、UID1001、Go1.22.2、Python3.12.3；未注入真实凭据、env或外部DSN。helper运行时锁与版本不变。
