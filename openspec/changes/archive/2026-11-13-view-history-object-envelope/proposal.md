# View 历史对象信封补录

## Why
View 刷新读取的历史接口实际返回 JSON 对象，而旧页面把每行当作字符串解析并静默丢弃，使已持久化的 Agent 输出不可见。本次补录已经交付的兼容修复及证据，把真实对象、旧字符串和无效行的行为写入可审查规格，避免只用字符串夹具造成历史显示回归。

需求来源：S-R59-CLOSEOUT-PREP（opus50tc2k.zzc，2026-11-13T11:05:53Z）；[修复提交](https://git.code.tencent.com/wego-ai/dingwei/commit/2e1e4c9db3c42cf7c987a548fa9214aad67a9f0c)；复盘分支 opus50tc2k/retro-view-refresh-20261113 @ 17834990309f05ffa6c9307a7ba954e5bd133ecc。

## What Changes
补录 console-session-history 的对象/字符串信封兼容与无效行告警；只改OpenSpec及文档，不重新实现、不部署。

## 待确认假设
无。历史原始形状与正反回放均有已有Git证据。

## 待 PM 决策
无。真实流量回放按rulings补112按需使用，不新增固定验收门。
