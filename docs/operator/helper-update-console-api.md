# helper 升级 Console 适配层（WIP）

这是一份开发合同，不是上线／升级指令。核心接口由 `review/togpt/helper-self-update-hub` 提供；本适配层不写 catalog、job、回执或确认状态。

## 路由与权限

仅新平台注册 `GET /api/console/helper-updates` 和 `POST /api/console/helper-updates`。使用现有 `RequireConsolePrincipal`：N3 登录、active 用户、强制改密与写操作精确 PublicOrigin 防护；handler 再检查 principal、active 及写 Origin。每个 active 用户只能操作自己名下的 Agent，manager/platform_admin 不因此获得跨 owner 升级权限。

POST 只接收 `{"release_sha256":"<已登记 SHA256>","agent_ids":["<Agent ID>"]}`。单个即一个 ID；最多50个、去重。拒绝未知字段（包括 URL、owner、job 或阶段），不从请求体获得权限。适配层逐目标重新查归属与状态，然后调用携带服务端 principal 的核心下发入口作二次授权。逐项返回 results（agent_id、ok、error、可选 job）。请求失败或未确认不能自动再次下发，应先刷新核实状态。

## 只读投影

GET 返回 releases（sha256、version、platform）和当前 owner 的 agents（agent_id、name、version、online、eligible、reason、可选 job）。job 仅含 job_id、version、stage、白名单 reason；不返回 URL、凭据、owner 信息、健康 nonce 或锁校验内部元组。使用核心只读目录／作业 API，不自行读取和解释 app_config 键值。

当前版本来自该 Agent 凭据绑定的在线连接；离线时只用归属匹配的持久 session_endpoint.version 作为显示值，不把离线记录当作新确认。首版新下发只开放给在线、自备、active、Linux 、helper 版本支持0.2.3.1更新协议且实际声明 session_capabilities.helper_update_v1 的目标；旧 helper／未知平台显示 manual_required，离线目标等待连接后再操作。已存在作业的恢复和有效期继续由核心负责。

阶段直接展示核心结果：offered 已下发，downloaded 已取包，installed 已安装待重启确认，restarted 已重启待版本确认，confirmed 已上线新版本；失败／回滚各自显示。不以安装回执冒充成功，也不由页面改阶段。

页面展开时才取目录，单个／批量都只选择已登记 SHA，无 URL 输入。15秒刷新仅在面板展开时运行；失败清除可操作快照，旧响应不覆盖新视图，选中目标失去资格或被移出列表时清除选择。所有远端文字用 textContent。

实际 Hub/helper/Docker 全链门以及核心 REWORK 属于组合候选，不能以浏览器 HTTP 拦截测试替代。
