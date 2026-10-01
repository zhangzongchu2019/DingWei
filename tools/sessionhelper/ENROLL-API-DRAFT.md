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
