# 客户端入网：实现对齐说明（接口以 G3 设计件为准）

- 作者：tosonnet50ts · 派工 R-NEWPLAT-G3-tosonnet50ts-20261001084334 · 对应 SPEC-THIN-SH-V1（含第七节修订）
- 接口来源：G3 承办 opus50tc2k 2026-10-01 的【暂定】接口（本文第 1、2 节已改为与之一致）；设计件落板后以设计件为准，改动处会另行告知
- 实现：`sessionhelper/enroll.py`；`UrllibTransport` 与 `enroll()` 里的两条路径是唯一依赖接口形状的地方

## 1 发起申请  POST /api/enroll/applications

    owner_user_id        使用者安装时填写（客户端配置）；只允许大小写字母与数字，2～32 位（台账 c5981300），客户端原样上送，Hub 按小写判定
    pairing_salt         16 字节随机，hex
    pairing_digest       sha256(salt 字节 + 6 位配对码 ASCII)，hex；配对码明文不上送
    device_fingerprint   见第 5 节
    client_version
  响应 201：application_id · poll_token（Hub 签发）· expires_at（UTC，RFC3339）

## 2 轮询  GET /api/enroll/applications/{application_id}，Authorization: Bearer <poll_token>

    state  pending | approved | rejected | expired | superseded
    approved 时：agent_name（保留大小写）· agent_full_name · credential（只给一次）
  客户端行为：间隔默认 4 秒；rejected／expired／superseded 即停止并在屏幕说明，**不自动重新申请**（重启客户端才重新申请）。
  approved 但 credential 为空（已被取走一次）⇒ 明确报错，要求业主吊销后重新申请；不写空凭据文件。

## 3 凭据落盘

credential.json（{agent_name, secret}）：目录 0700、文件 0600，创建即 0600，先写临时文件 fsync 再 rename。写成功后才删除 pending 状态（poll_token 随之丢弃）。读取时权限宽于 600 ⇒ 拒绝使用，不自动改权限，报错不带凭据值。

## 4 以专属凭据连入（组长裁定 2026-10-01，台账 bfc84543）

只带专属凭据：`Authorization: Bearer <credential>`；身份由 Hub 按凭据查出，客户端不自报名称、不带账号 key。`enroll.agent_ws_url` 暂用占位路径 `/ws/agent`，查询串只含 version 与能力类参数，并主动剥掉 key_id／name 类字段。路径待 Hub 设计件定。
目前只有桩：`run_enrolled(connect=...)` 把凭据交给传入的连接函数；真连接待设计件。

## 5 设备指纹（我这边定的算法，请 Hub 侧知悉）

首次生成 128 位随机数，存 `enroll-device.id`（0600）；上送其 sha256（hex）。同一安装多次申请相同；重装系统或清掉状态目录则变化——这是"同设备"在客户端侧的边界，不是硬件指纹。若要求更强的设备绑定（读 machine-id 等），请说明，会涉及隐私与容器重建后的稳定性取舍。

## 6 吊销（V2）

`revoke_local(state_dir, notify)`：删除凭据文件与 pending 状态、屏幕提示、不自动重新申请。触发点（收到"已吊销"通知／连接被拒且原因为已吊销）待连接鉴权设计件给出原因码后接入。

## 7 本地文件（SH_STATE_DIR，默认 ~/.dingwei）

    credential.json · enroll-pending.json（配对码、application_id、poll_token、expires_at；0600，重启沿用同一配对码）· enroll-device.id

## 8 提醒

配对码仅 6 位（10^6），摘要泄露后可离线穷举；保护靠作废时限、来源限频、业主在场核对，Hub 侧勿视摘要为秘密。

## 9 托管席位入网入口（组长令 2026-10-01 09:46Z；对应 opus50tc2k/g3-hosted-cred 的 IssueHostedAgent）

Hub 新建托管 Agent 时直接签发一次性入网令牌＝一条已批准的申请 {application_id, poll_token, expires_at}（2 小时有效）。容器里的客户端**跳过"生成配对码、发起申请"**，直接用它轮询取凭据。
- 入口：`enroll.enroll_with_token`；`SH_ENROLL=1` 且存在令牌文件（`SH_ENROLL_TOKEN_FILE`，默认 `<状态目录>/enroll-token.json`）时由 `prepare_client_env` 自动选用；此时不需要 `SH_ENROLL_OWNER`，也不显示配对码。
- 令牌文件：JSON `{"application_id": …, "poll_token": …, "expires_at": RFC3339}`，由节点代理在容器里一次性写入；**不经环境变量、不进镜像层**（环境变量里至多放文件路径）。
- 读完即删：先把状态存入 `enroll-pending.json`（0600，kind=hosted，重启可续轮询），再删除令牌文件；存不下状态则不删（留给下一次重试）。令牌文件读不出或缺字段则删除并报错，不留半截秘密。
- 轮询、`superseded／expired／rejected` 停止、429 等待、凭据 600 原子落盘、取过一次再取为空（明确报错）——全部复用自备路径同一段代码。
- 过期：Hub 回 expired ⇒ 明确失败、清理状态、不留半成品；令牌文件里的 expires_at 明显已过（超出 5 分钟时钟偏差宽限）则不再打扰 Hub，直接同样失败。
- 轮换（容器重建、升级时 `ReissueHostedToken`）：新令牌文件优先于已有凭据；新凭据被真正取回之前旧凭据原样保留，取回失败不破坏旧的。
- 请 G4 节点代理确认：令牌文件路径与权限（建议 0600、属容器内运行用户）、写入时机（在客户端启动前）。
