# r55混精度终态护栏

## Why

已复核24b4b644分析确认微秒live入场回执与秒级native终态混用时，helper可能先processed而Hub拒绝其终态ACK。需在r55发布前以最小本地屏障保留pending和可诊断原因，避免生成必拒回执及状态分裂，不借修复改原时刻或扩大协议。来源：rulings〔51〕-补7 c2b64423及S-R55-MIXED-PRECISION-GUARD派单。

## What Changes

仅helper两产品文件和对应测试，增加同事务wire先后比较、pending原因与Adapter拒绝terminal观察；矩阵8控保持、mixed转pending，原生证据不写改。无迁移/新运行配置/Hub/View/版本变更，1s/15s/200不动。

## Capabilities

### New Capabilities
- helper-terminal-ack-guard：终态回执对原injected边界的护栏。

## Impact

只改tools/sessionhelper。此目录为任务内OpenSpec交审项目，不改仓根openspec；产品/测试/原文与范围见IMPLEMENTATION.md。自动查新证据/人工修历史并未授权，发布放行由组长独立复核。

## 待确认假设

A01：没有持久发送位，outbox保守视为可能已发；不为区分未发新增schema。

## 待 PM 决策

P01：独立复核后是否恢复r55分发；本席不自验收。
P02：若真实终态始终不兼容，人工对证与r56时钟语义另排，不以等待伪造新时间。
