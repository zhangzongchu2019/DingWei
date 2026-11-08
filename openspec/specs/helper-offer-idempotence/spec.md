# helper-offer-idempotence Specification

## Purpose
本变更把helper首offer的并发处理从可能把重复冻结当作错误或再次写入，改为按同一活动job的持久首次连接做幂等返回，目的是保留升级重启确认的真实边界。涉及Hub首offer记录及回执前后的并发场景，不影响身份授权、升级阶段协议、数据库schema或发布流程。

## Requirements

### Requirement: HOF-001 同一活动job首offer冻结幂等
Hub SHALL 以第一次成功CAS冻结的OfferConnSeq为边界；读到同一活动job已冻结时，或CAS失败后重读到同一活动job已冻结时，MUST 返回当前持久job且不再次写库、不增加Revision。job被替换、已终态、读取失败或仍未冻结时不得伪称幂等成功。

#### Scenario: HOF-S001 两方并发首offer
- **WHEN** 两方持同一未冻结快照，序号20先完成CAS，序号10的旧CAS失败
- **THEN** 两方得到首次边界20，只有一次offer写入；中间已出现的installed回执及其Revision不得覆盖

#### Scenario: HOF-S002 冻结后的陈旧快照
- **WHEN** 同一初始未冻结快照在offered、downloaded或restarted阶段再次记录offer
- **THEN** 首次连接、阶段、激活连接和Revision不变，不把额外Revision归咎于陈旧receipt

#### Scenario: HOF-S003 失败不能扩大幂等条件
- **WHEN** CAS失败后重读到另一job、终态job或仍未冻结的job
- **THEN** 保留拒绝且不覆盖该持久新状态
