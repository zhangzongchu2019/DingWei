## ADDED Requirements

### Requirement: R58I-001 r58冻结输入组合验证
r58集成 MUST 从153bac4d按派单顺序保留五项原tip各一个no-ff merge，SHALL 实测并记录helper库存与expected/actual。最终tip MUST 通过全仓Go、hosted race三轮、canonical双profile、全量浏览器和strict，并分别撤防根绑定与Claude归属以证明判据有效。不得改变版本位或部署。

#### Scenario: R58I-S001 最终组合交审
- **WHEN** 五项合并且计数钉值已核
- **THEN** 所有门在同一最终tip执行，交件包含五个merge、tip/tree、读数、跳过原因及快进推送后远端一致证明
