# Proposal

## Why

View 当前分别维护 Hub 序号节点与独立时间卡，未序号输入卡会被追加到同一会话最早回合之前，造成较新的输入显示在旧输出上方。修复将回合组和独立卡合并到统一时间轴，并以完整 DOM 顺序作为可验证结果，避免只检查卡片子集而漏掉跨类型倒序。

需求来源：2026-11-13 `S-VIEW-INPUT-MISSING` 退回意见（评审席 opus50tc2k.zzc，任务消息坐标）；相关先前排序实现：[6b854bbb27b19aa84897cc6652681fdc5c58d9b5](https://git.code.tencent.com/wego-ai/dingwei/commit/6b854bbb27b19aa84897cc6652681fdc5c58d9b5)。

## Scope

修复 Console View 顶层 timeline 节点排序，新增七张无 seq 输入卡与相邻回合组的单调性回归。保留同 Hub epoch 使用 seq 的规则。

## 待确认假设

- 跨 epoch 或跨流的时间比较采用每个顶层节点首个可见 `.event-time[data-ts]`，与评审判定器口径一致。

## 待 PM 决策

- 无。
