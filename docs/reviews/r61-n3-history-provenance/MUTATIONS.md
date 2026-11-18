# N-3 作者删保护（最终源）

SOURCE_COMMIT `a32c877cf6ef21a909d2806f4b08e3e925d05d98`；40个有效cut，40个业务断言KILLED，0幸存，0编译错/panic；逐刀原文/命令/UTC/日志SHA见evidence/mutations/summary.json。首尾基线rc0，还原后工作树clean。历史31/34/38/40(D181)不合并计数。

4KiB单一契约有Go与SQL CHECK两道冗余防线，此cut明确同时去掉两处，additional_edit单记；不把它冒称单行改动。

| cut | 文件 | rc | 结果 |
|---|---|---:|---|
| association_owner_key | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| association_agent_key_id | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| association_session_name | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| association_generation | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| association_delivery_id | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| admitted_only | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| user_input_only | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| same_delivery_origin | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| nonempty_turn | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| stored_trust | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| conflict | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| expiry_match | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| strip_client_claim | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| consume | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| oversize_rollback | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| agent_quota | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| global_quota | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| ttl_cleanup | internal/store/session_history.go | 1 | 业务断言红，编译成功，无panic |
| identity_cleanup | internal/store/session_history.go | 1 | 业务断言红，编译成功，无panic |
| disable_cleanup | internal/store/session_history.go | 1 | 业务断言红，编译成功，无panic |
| queued_eviction | internal/store/session_history.go | 1 | 业务断言红，编译成功，无panic |
| sidecar_eviction | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| generation_cleanup | internal/store/history_migrations/0078_history_provenance.sql | 1 | 业务断言红，编译成功，无panic |
| global_insert_accounting | internal/store/history_migrations/0078_history_provenance.sql | 1 | 业务断言红，编译成功，无panic |
| agent_insert_accounting | internal/store/history_migrations/0078_history_provenance.sql | 1 | 业务断言红，编译成功，无panic |
| global_consume_accounting | internal/store/history_migrations/0078_history_provenance.sql | 1 | 业务断言红，编译成功，无panic |
| agent_consume_accounting | internal/store/history_migrations/0078_history_provenance.sql | 1 | 业务断言红，编译成功，无panic |
| primary_only | internal/m8/terminal.go | 1 | 业务断言红，编译成功，无panic |
| hub_persist_dom | internal/m8/terminal.go | 2 | 业务断言红，编译成功，无panic |
| append_field_dom | internal/store/session_history_provenance.go | 2 | 业务断言红，编译成功，无panic |
| renderer_attestation | view-service/renderer-canary-sse.html | 2 | 业务断言红，编译成功，无panic |
| oversize_gap_kind | internal/store/session_history.go | 1 | 业务断言红，编译成功，无panic |
| sidecar_no_gap | internal/store/session_history.go | 1 | 业务断言红，编译成功，无panic |
| sidecar_4KiB_boundary | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| capture_target_snapshot | internal/m8/history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| capture_key_scope | internal/m8/history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| capture_session_scope | internal/m8/history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| route_passes_captured_target | internal/m8/hub.go | 1 | 业务断言红，编译成功，无panic |
| cleanup_quantum | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
| eviction_quantum | internal/store/session_history_provenance.go | 1 | 业务断言红，编译成功，无panic |
