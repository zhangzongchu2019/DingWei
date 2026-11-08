# 原ACK边界现状

## Purpose

记录既有helper原始ACK按阶段持久保留及Hub终态先后约束，供最小混精度护栏的兼容检查使用。涉及helper账本和回执原文，不改变现有Hub协议、迁移、原生时刻或已确认终态。

## Requirements

### Requirement: HTB-001 原ACK不可重建
既有helper ACK outbox SHALL 保留已生成阶段payload，确认后由cursor记录阶段；重放不重取发生时刻。Hub processed先后校验不因本地证据补取而放松。

#### Scenario: HTB-001-S01 已确认阶段
- **WHEN** injected已确认，随后收到native终态
- **THEN** 不重新生成已确认injected或将其发生时刻改成新值
