# r57 集成冲突记录

基线 main 3b6157859f5a329026bfcf43bf95de81909919f8；本件仅集成，不改 VERSION、不构建发布件、不部署。五个冻结提交不改写、不rebase，按派单顺序no-ff合并。

前三项serverInfo、LEN重锚、PG登记无冲突。ZERO-CONSENT c116424367c7f383a9dbf63c3ce0320169c808d7 合并：8文件21处；以下原始行号指自动合并冲突现场，最终行号可能移动。

| 编号 | 文件／原始冲突行 | 语义取舍与理由 |
| --- | --- | --- |
| Z01 | `cmd/workpulse/migrate_refuse_test.go` 第1处（原行75） | 空库断言取14条已上线集合与0108的并集，更新为15条；保留两侧完整条目说明。 |
| Z02 | `cmd/workpulse/migrate_refuse_test.go` 第2处（原行89） | 重启仍15条且不增账，保留幂等判据。 |
| Z03 | `cmd/workpulse/np_session_migration_binary_test.go` 第1处（原行42） | r50旧库夹具须同时排除0108、0110、0111等后续条目，保留r50起点8条语义。 |
| Z04 | `cmd/workpulse/np_session_migration_binary_test.go` 第2处（原行100） | 升级后断言15条及0108/0110/0111并列，不删既有created_at与旧二进制兼容判据。 |
| Z05 | `internal/feishulogin/types.go` 第1处（原行197） | CallbackRequest取字段并集：State/Code、Selection/Target与服务端AttemptID；不从query引入主体或AttemptID。 |
| Z06 | `internal/m8/claim_client_integration_test.go` 第1处（原行74） | Hub真PG夹具按顺序保留0108/0110/0111的并集，其他Claim链路判据不变。 |
| Z07 | `internal/mcpgw/oauth.go` 第1处（原行240） | 保留GET state与POST selection两入口、参数唯一/长度/用途前缀；错误出口用main的failCallback安全分类，其他HTTP方法仍405。 |
| Z08 | `internal/mcpgw/oauth.go` 第2处（原行288） | 同时保留浏览器/attempt复核、一次性命名空间Take、服务端AttemptID绑定、选择页分支与main错误原因/目标分类；选择不提前消费OAuth attempt，完成签发才消费。 |
| Z09 | `internal/store/migrate/domains_test.go` 第1处（原行117） | Hub硬编码名单并入0108，保留0110/0111，禁止fanout混入。 |
| Z10 | `internal/store/migrate/domains_test.go` 第2处（原行222） | 空库施加名单取并集，保留main的全量排序判据。 |
| Z11 | `internal/store/migrate/domains_test.go` 第3处（原行241） | Hub总数从14到15，既有逐条digest校验保持。 |
| Z12 | `internal/store/migrate/domains_test.go` 第4处（原行306） | 旧identity基线后的待施加列表加入0108，保留0110/0111。 |
| Z13 | `internal/store/migrate/domains_test.go` 第5处（原行328） | 已记两基线后的增量条数从12到13，保留旧账逐条不变判据。 |
| Z14 | `internal/store/migrate/ledger_test.go` 第1处（原行55） | Hub硬编码名单取并集15条，保留PG账本文案修正。 |
| Z15 | `internal/store/migrate/ledger_test.go` 第2处（原行92） | 重复施加应15条空转，Applied仍0。 |
| Z16 | `internal/store/migrate/ledger_test.go` 第3处（原行212） | 轨A增量DDL同时保留0108/0110/0111；0108仅改CHECK，不加表，表数仍40，view文案保留。 |
| Z17 | `internal/store/migrate/migrate_test.go` 第1处（原行148） | 全账本名单16条，保留0108/0110/0111与独立fanout0400。 |
| Z18 | `internal/store/migrate/migrate_test.go` 第2处（原行246） | 动态Register排序夹具并入0108，保留其余冻结条目与测试域条目。 |
| Z19 | `internal/store/migrate/migrate_test.go` 第3处（原行616） | 并发迁移全账本数为16，保持各条恰好一次。 |
| Z20 | `internal/store/migrate/migrate_test.go` 第4处（原行665） | 并发终态16条、digest与CheckSchema判据保留。 |
| Z21 | `internal/store/migrate/migrate_test.go` 第5处（原行686） | 再跑应16条空转，无重复施加。 |

## 自动合并区域的语义补齐

- feishulogin/handler.go 的公共issue抽取后，main的安全错误分类仍依赖attemptID；在issue内从可信LoginAttempt.Request重新取值，避免作用域丢失。
- feishulogin/defaults.go 的selection消费补同一服务端AttemptID关联校验；GET回调继续由main既有判据校验，POST不得绕过该关联。
- defaults_test新增空允许集合的旧入口/八位上限/手填签发用例；既有TestConsentSingleAndDisabled仍覆盖缺席、空、其他租户。集成配置没有写任何初始允许目标。
- consent_test新增先施加含0110/0111的14条，再补0108且唯一Applied=0108的用例；核旧14行digest与applied_at不变、再启动15条空转。0108不新增表，因此双轨表数保持40；全PG账本16、Hub15，SQLite仍接0074后的0075/0076。

## M3 冲突

待合并第五项后补记。
