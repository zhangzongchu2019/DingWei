# 飞书按需镜像同步（#sync）规格 v0.1

> 目标：会话 I/O **默认不镜像飞书**（有 `/view` 网页终端可看）；需要时飞书发 `#sync <会话名>` **按需开启**，并**一次性补发最近 10 条历史（每条带时间）**；`#unsync <会话名>` 停止。

## 一、默认行为变更
- **去掉各会话的 `SH_MIRROR_TO`** → 默认不再把 CLI I/O 刷到飞书；
- 看会话实时画面用 `/view`；要在飞书里跟看某会话，才 `#sync`。

## 二、命令（飞书私聊 UnifiedRobot 发出）
- **`#sync <会话名>`**：开启把该会话的 terminal I/O 镜像到**发起人的飞书**；开启时**立即补发该会话最近 10 条历史（每条带时间戳）**，之后实时转发；
- **`#unsync <会话名>`**：停止该会话对本发起人的同步；
- 目标飞书 = 发指令者的 open_id（谁发同步给谁）。

## 三、🆕 地址简化（仅飞书侧指令）
- 飞书侧发 `#sync <会话名>` **免 key 前缀**：在**发起人自己 owner 名下**解析该会话名（复用已有 owner 路由 `resolveOwnerSessionKey`）；
- **跨用户寻址**（同步别人的会话，若允许）**必须显式给出** `#sync <会话名>#<key>`；
- 此简化**仅限飞书侧**入站指令；会话间 send.py/`#会话名` 路由维持现状。

## 四、🆕 Hub 侧最近 10 条缓存
- Hub 为**每个会话**维护一个**环形缓冲**，保存最近 **10 条** `terminal_output` 片段；
- **每条附 UTC 时间戳**（片段产生时间）；
- `#sync` 时把这 10 条**按时间顺序、逐条带时间**发给发起人飞书（补上下文），再转入实时；
- 缓冲随新片段滚动更新；会话下线清理。

## 五、Hub 侧机制
```
syncTargets: map[会话key] -> set(发起人飞书open_id...)   // 谁在同步这个会话
recentBuf:   map[会话key] -> ringbuffer(最近10条{ts, text})

收到 #sync <会话名>（来自飞书 open_id X）:
  target = 解析会话(owner=X的owner, name, 可选#key)         // 免前缀:X自己owner下解析
  syncTargets[target].add(X)
  for item in recentBuf[target]:  发飞书(X, "[item.ts] item.text")   // 补发10条带时间
  回执 X: 已开启同步 <会话名>

收到会话的 terminal_output（来自会话 S）:
  recentBuf[S].push({now_utc, text})                        // 滚动缓存
  for X in syncTargets[S]:  发飞书(X, text)                  // 实时转发给所有同步者

收到 #unsync <会话名>（来自 X）:
  syncTargets[target].discard(X); 回执 X: 已停止
```

## 六、边界
- 同一会话可被多人同步（各自 open_id）；
- 会话下线：清 recentBuf + syncTargets（或标记，重连后不自动恢复同步，需重新 #sync）；
- 飞书纯文字（无 markdown/表格）；补发的 10 条与实时片段都带时间前缀 `[YYYY-MM-DD HH:MM:SS UTC]`；
- 转发限流/合并：短时间大量输出可合并片段，避免刷屏（沿用 terminal_loop 的 120ms 合并思路）。

## 七、验收（Linux + macOS 双平台）
1. 默认（无 SH_MIRROR_TO）：会话 I/O **不进飞书**，/view 正常；
2. 飞书发 `#sync zzc-manager`（**免 key 前缀**）→ 收到**最近 10 条历史（每条带时间）** + 之后实时；
3. `#unsync zzc-manager` → 停止；
4. 跨用户：`#sync <别人会话>` 无 key → 拒/提示需显式 key；带 `#key` → 按规则处理；
5. 多人同步同一会话互不影响；
6. 会话下线后缓存/同步清理，重连需重新 `#sync`。
