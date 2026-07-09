# 自助申请 key（#申请）+ 审批发放 规格 v0.1

> 目标：新人在飞书发 `#申请 <说明>` → 走审批（审批人批准）→ 自动签发 key + 绑定其飞书账号 + 私聊发回 **key + 三平台(Linux/Mac/Windows-WSL2)安装配置指引**。
> 现状：`#申请` L1 规则已登记，但 hub 里 `l1_command_apply_key` 只走通用成员路由（桩），**未真正发 key**。本规格把它补实。

## 一、命令
- **`#申请 <说明>`**（新人飞书私聊 UnifiedRobot）：提交一条 key 申请（说明=用途/机器/角色）；
- **`#批准 <申请ID>`**（仅审批人=配置的审批人 open_id）：签发 key + 绑定 + 私聊发放；
- **`#拒绝 <申请ID> [原因]`**（仅审批人）：驳回并通知申请人。

## 二、流程
```
新人发 #申请 说明:
  建 pending 申请记录(id, applicant_open_id, applicant_name, note, status=pending, created_at UTC)
  回申请人: "已提交申请 <ID>，等待审批"
  通知审批人: "新key申请 <ID> 来自 <名/open_id>：<说明>。批准回 #批准 <ID>，驳回 #拒绝 <ID>"

审批人发 #批准 <ID>:
  校验发起人是审批人 open_id（否则拒）
  在默认租户下签发 service key（key_id + secret，secret 仅此一次）
  绑定 key → 申请人飞书账号(open_id)  // 复用 admin 签发+绑定逻辑
  标记申请 status=approved
  私聊申请人: [key 信息] + [三平台安装配置指引]（见§四）
  回审批人: "已批准 <ID>，key 已发放"

审批人发 #拒绝 <ID> 原因:
  status=rejected；私聊申请人驳回+原因
```

## 三、🔴 安全
- **仅审批人**（配置的 open_id）能 `#批准/#拒绝`；别人发→拒并记日志；
- secret **只在私聊发一次**，不落明文日志/不广播；
- 申请记录落库审计（谁、何时、批/拒、key_id，不存 secret）；
- 防刷：同一 open_id 短时间重复 `#申请` 合并/限流。

## 四、审批通过后私聊发放内容（飞书纯文字，时间 UTC）
```
【DingWei 接入已开通】
你的 key_id: <key_id>
你的 secret: <secret>  （只显示这一次，请立即保存）
会话名建议: <你的名字拼音，如 zhangsan>

—— 安装配置 DingWei 客户端(sessionHelper) ——

[Linux]
1. 下载接入包: curl -fsSL https://ts.wegoab.com/dl/dingwei-linux.tar.gz -o ~/dingwei.tgz && tar xzf ~/dingwei.tgz -C ~/dingwei
2. 前提: 本机已装并登录好 AI CLI(claude/codex/opencode 任一)
3. 改 config: 填 SH_KEY_ID=<key_id> SH_SECRET=<secret> SH_SESSION_NAME=<会话名> SH_CLI=<cli> SH_CLI_CWD=<工作目录>
4. 首次信任工作目录(claude 需要): cd <工作目录> && claude --dangerously-skip-permissions → 选“信任” → /exit
5. 启动: cd ~/dingwei && WORKPULSE_SH_CONFIG_FILE=$PWD/config ./sessionhelper/run.sh
6. 自愈: 配 cron 每2分钟跑 guard.sh(掉线自愈)

[macOS]
1. 前提: 装好 claude + 包装脚本(key 写死进脚本，勿用变量)
2. 下载: env -u SSL_CERT_FILE curl -fsSL https://ts.wegoab.com/dl/dingwei-mac.tar.gz -o ~/Downloads/dingwei-mac.tar.gz
3. 解压: mkdir -p ~/dingwei && tar xzf ~/Downloads/dingwei-mac.tar.gz -C ~/dingwei
4. 改 config-*: 填 key_id/secret/会话名/CLI/工作目录
5. 首次信任工作目录(=SH_CLI_CWD，不是安装目录): cd <SH_CLI_CWD> && claude-deepseek --dangerously-skip-permissions → Enter信任 → /exit
6. 后台运行: cd ~/dingwei && ./deploy-macos/install-launchd.sh "$PWD/config-你的"
   看日志: tail -f ~/.dingwei/logs/<会话名>.log

[Windows —— 用 WSL2(推荐，零改)]
1. 装 WSL2 + Ubuntu: 管理员 PowerShell 跑 wsl --install，重启，装 Ubuntu
2. 进 WSL2 的 Ubuntu 终端，按上面 [Linux] 步骤操作(sessionHelper 在 WSL2 里和 Linux 完全一致)
3. AI CLI 也装在 WSL2 内

需要帮助: 飞书私聊 UnifiedRobot 发 #帮助，或联系管理员。
```
> 说明：三平台文案作为**响应模板**内置（可配置/可后续调整）；下载链接以实际为准（Linux 包若未就绪，先给现有 onboarding 方式）。

## 五、验收
1. 新人 open_id 发 `#申请 测试` → 收“已提交 <ID>”，审批人收通知；
2. 审批人 `#批准 <ID>` → 申请人私聊收到 key(secret 一次)+ 三平台指引；DB 有审计；
3. 非审批人 `#批准` → 拒；
4. `#拒绝 <ID> 原因` → 申请人收驳回+原因；
5. secret 不出现在任何日志/广播；
6. 三平台指引文案完整(Linux/macOS/WSL2)。
