# provision 发送方(服务端)规格 v0.1

> 背景:provision 客户端(sessionHelper)已能**接收+执行** update_self/install_skill/install_mcp(见 provision.py,已上线 2.1.0)。
> 缺口:**服务端没有"发送 provision 指令"的入口** → 现在没法触发一次真实下发。本规格补齐发送侧,让下发通道闭环、可 dogfood。

## 一、Hub 发送方法
```
func (h *Hub) SendProvision(ctx, keyID, sessionName, action, url, sha256, version, target string, extra map) error
  构造信封: from="workpulse#"+keyID, to=sessionAddress(sessionName,keyID),
    meta={ type:"provision", system:true, no_mirror:true,
           action, url, sha256, version, target, extra }
  发给该会话的 sessionClient(c.write);会话离线则报错/记待发。
```
> 客户端 `validate_source` 要求 `from=workpulse#<key>` 且 `system=true`——本方法天然满足,别的来源发不了(安全)。

## 二、Admin 页面 /admin/provision(鉴权同其它 admin 页)
- **目标选择**:①单个会话 ②某账号(owner)全部在线会话 ③某 key 全部会话(fleet 批量);
- **字段**:action(update_self/install_skill/install_mcp)、url、sha256、version、target、extra(JSON);
- **便捷**:可选"上传制品"→服务端存到 `/dl/` + 自动算 sha256 回填(省手工算);
- **提交**:对每个目标会话调 `SendProvision`;结果表格显示每会话发送成功/失败;
- **审计**:谁在何时对谁发了什么(action/version/sha256)落库 + 日志。

## 三、制品打包助手(给 update_self 用)
- 一个 admin 动作/脚本:**打包当前仓的 sessionHelper**(tools/sessionhelper)→ tar → 放 `/dl/sessionhelper-<version>.tar.gz` → 算 sha256;
- update_self 的 url 指向它、version 用 `__version__`、sha256 用算出的值;
- 客户端收到后:sha256 校验→版本 > 当前才装(防降级已在客户端)→替换重启→失败回滚。

## 四、fleet 批量下发
- "某账号全部在线会话"→ 遍历该 owner 的在线会话逐个 `SendProvision`(复用 owner 跨 key 解析);
- 建议**灰度**:先发 1 台看回执 OK,再批量(避免一条坏指令打爆全网);
- 每台回执(ProvisionResult:ok/version/error)收集展示。

## 五、🔴 安全(补充客户端已有的之外)
- 触发权限:**仅 admin**(经 admin 鉴权)能发 provision;
- url host 必须在客户端白名单内(`SH_PROVISION_ALLOWED_HOSTS`),发送前服务端也校验一遍;
- version 单调:建议服务端也记录已下发版本,不发更低版本(双保险)。

## 六、验收(给 tester,Linux + macOS 双平台)
1. admin 发 install_skill(带真 sha256)→ 目标客户端装上 skill + 回执 ok + 审计有记录;
2. sha256 错 → 客户端拒 + 回执 failed;
3. 发 update_self(打包当前+1的版本)→ 客户端 sha256 过→版本更高→替换重启→**新版连回报 version=新**;造"新版起不来"→**回滚旧版**;
4. 发过低版本 update_self → 客户端防降级拒绝(回执"newer or equal");
5. 非 admin/非 workpulse 来源 → 发不出/被拒;
6. fleet 批量:灰度 1 台 OK 再全量,回执齐;
7. Linux(美洲机)+ macOS(zzc-mac)各验一遍。
