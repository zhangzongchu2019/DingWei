# 入网接口草案（客户端侧所需，供 Hub 侧 opus50tc2k 对照；以 Hub 定稿为准）

- 起草：tosonnet50ts · 派工 R-NEWPLAT-G3-tosonnet50ts-20261001084334 · 对应 SPEC-THIN-SH-V1（含第七节修订）
- 客户端实现：`sessionhelper/enroll.py`；传输层 `UrllibTransport` 是唯一依赖下列路径的地方，改路径只动这一处

## 1 申请  POST /api/enroll/apply（无需认证，Hub 须按来源限频）

请求体（JSON）：

    owner            申请所属业主（小写字母数字）
    pairing_salt     客户端随机盐
    pairing_digest   sha256(pairing_salt + ":" + 配对码) 的十六进制；Hub 不收配对码明文
    poll_digest      sha256(poll_secret) 的十六进制；poll_secret 只在客户端
    device_digest    设备指纹的 sha256（同一设备重复申请据此归并，判据 A10）
    client_version   客户端版本
    capabilities     客户端自报（自备模式不含 CLI 选择；字段可空）

应答：`{"request_id": "<不可猜的串>"}`
Hub 记录：来源 IP、申请时刻（A1）。24 小时作废（A7）。

## 2 取回  GET /api/enroll/{request_id}，请求头 X-Poll-Secret: <poll_secret>

Hub 用 sha256(X-Poll-Secret) 与 poll_digest 常量时间比较，不符一律 404（不泄露 request_id 是否存在）。

应答 status 取值：

    pending    等待业主批准
    approved   附 agent_name 与 credential（专属凭据，**只在此应答出现一次**，之后该请求不再返回凭据）
    rejected   业主拒绝（＝踢走）
    expired    24 小时作废；客户端自动重新申请并显示新配对码

## 3 业主批准时的校验（Hub 侧，客户端不参与）

业主在 console 输入的配对码 → 用该申请的 pairing_salt 算摘要 → 与 pairing_digest 常量时间比较；不符拒绝且申请保留（A2）。配对码一次性：批准成功即失效。
注意：6 位码只有 10^6 种，摘要库一旦泄露可被离线穷举——配对码的保护靠"24 小时作废＋限频＋业主在场核对"，不靠摘要；请 Hub 侧勿把它当作秘密存储对待。

## 4 以专属凭据连入（组长裁定 2026-10-01，台账 bfc84543）

连接**只带专属凭据**：`Authorization: Bearer <credential>`，不再带账号 key。Agent 身份（名称.业主）由 Hub 按凭据查出；客户端不在连接地址里自报名称，即使带了也以凭据为准（自报名称正是冒名入口）。
客户端实现 `enroll.agent_ws_url`：路径暂为占位 `/ws/agent`，查询串只含 version 及能力类参数（tool、os 等），并主动剥掉 key_id／name 类字段。WebSocket 路径请 Hub 侧定稿。

## 5 本地文件（客户端，状态目录 SH_STATE_DIR，默认 ~/.dingwei，目录 0700）

    credential.json       {agent_name, secret}，权限 600，原子写入（创建即 600，无先写后改权限的窗口）
    enroll-pending.json   申请进行中的状态（配对码、盐、poll_secret、request_id），600；重启后沿用同一配对码
    enroll-device.id      设备指纹原值，600；上送的是其 sha256
读取时若 credential.json 权限宽于 600 ⇒ 拒绝使用并报错（不自动改权限，不在报错里带凭据值）。
