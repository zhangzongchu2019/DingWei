# sessionHelper 自动下发通道(provision)规格 v0.1

> 目标：中央 DingWei 按指令下发,让每台 sessionHelper 能**自更新、装 skill、装 MCP**——终结手动 scp/tar。
> 🔴 这是**远程下载即执行(RCE 级)**能力,安全是第一约束。

## 一、总体
中央发 provision 信封 → sessionHelper 验来源+校验 → 下载 → 装到目标 → 回执(结果/版本)。

```
中央(workpulse) ──provision信封──▶ sessionHelper
   {action, url, sha256, target, version, ts}
sessionHelper: ①验来源(仅 from=workpulse/system) ②下载 ③校验sha256 ④装 ⑤回执
```

## 二、信封格式(meta.type = "provision")
```json
{
  "to":"<会话>#<key>", "from":"workpulse#<key>",
  "meta": {
    "type":"provision", "system":true, "no_mirror":true,
    "action":"update_self | install_skill | install_mcp",
    "url":"https://ts.wegoab.com/dl/<artifact>",     // 下载地址(白名单host)
    "sha256":"<hex>",                                 // 必带,校验
    "version":"<语义版本或hash>",                      // 幂等/防降级
    "target":"<skill名/mcp名/‘self’>",                // 装到哪
    "extra":{}                                        // 各action的附加参数
  }
}
```

## 三、三个 action

### A. update_self（自更新 sessionHelper）
1. 下载新版 sessionHelper 包(tar)到临时目录;
2. **校验 sha256** + 版本 > 当前(防降级/重放);
3. **备份当前代码**;
4. 原子替换代码(不动 venv/config);
5. **重启自身**(exec 新进程 / 让 guard·launchd 拉起);
6. **失败回滚**:新版起不来(如 N 秒内没成功连回 DingWei)→ 恢复备份 + 重启旧版 + 回执 failed;
7. 回执:`{action:update_self, ok, from_version, to_version}`。

### B. install_skill（装 skill）
1. 下载 skill 包(tar/单文件)→ 校验 sha256;
2. 按 CLI 类型装到对应 skill 目录:claude=`~/.claude/skills/<target>/`、codex=`~/.codex/skills/<target>/`、opencode=其机制(见 SH_CLI 判定);
3. 回执 `{action:install_skill, target, ok}`;下次会话可用。

### C. install_mcp（装 MCP）
1. 下载 MCP 配置/包 → 校验 sha256;
2. 写入对应 CLI 的 MCP 配置(claude=`~/.claude.json`/`mcp.json`、codex=`config.toml [mcp_servers]`),密钥走 env 占位不入库;
3. 回执 `{action:install_mcp, target, ok}`。

## 四、🔴 安全模型(必做,不可省)
1. **来源鉴权**:只接受 `from=workpulse#<本会话key>` 且 `meta.system=true` 的 provision;别的一律拒并记日志;
2. **完整性**:每个下载物**必带 sha256**,不匹配不落地;可选 Ed25519 签名(P2);
3. **来源白名单**:url 的 host 必须在白名单(如 `ts.wegoab.com`);
4. **幂等/防降级**:version 记录,已装过或版本更低→跳过;
5. **审计**:每次 provision(action/target/version/结果)落库 + 日志;
6. **最小权限**:install_skill/mcp 只写对应目录;update_self 只替换代码不碰用户数据/密钥。

## 五、分期
1. **P1 update_self**(收益最大最痛):sha256+备份+重启+失败回滚+审计;来源鉴权+白名单从第一期内建;
2. **P2 install_skill**;
3. **P3 install_mcp** + 可选签名。

## 六、与现状
- 现有 sessionHelper 已会自装 send.py(`install_send_script`)、写清单文件——把这套**泛化**成"按 provision 指令装任意物"即可;
- 下发触发:中央 admin 手动发 / 或结合"自助 onboarding"自动发。

## 七、🔴 跨平台(Linux + macOS,必须同时支持)
- **OS 识别**:复用 config.py `detect_os()`(linux/macos);
- **update_self 重启/自愈**:按平台选正确机制——**Linux** 由 cron+guard 拉起、**macOS** 由 launchd(KeepAlive)拉起;自更新时 exec/退出后要能被对应机制正确拉起新版(别只写死一种);
- **下载 SSL**:macOS Python 需 certifi CA(run.sh 已自动设 `SSL_CERT_FILE`,下载复用同一环境,别再踩证书坑);
- **路径**:全部 `os.path.expanduser("~")`(mac=`/Users/x`、linux=`/home/x`),不写死;
- **skill/mcp 目录**:两平台一致(`~/.claude/skills`、`~/.codex/skills`、`~/.claude.json` 等),无需分叉;
- **下载工具**:用 Python 标准库(urllib)而非依赖系统 curl(避免 macOS curl 的 CAfile 坑);
- 验收要在 **Linux(美洲机)+ macOS(zzc-mac)** 各跑一遍。
