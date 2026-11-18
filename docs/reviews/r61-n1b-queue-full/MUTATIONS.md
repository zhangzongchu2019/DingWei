# Mutation results

All nine mutations returned rc=1 on a behavior assertion (no compile failures). Each is an isolated temporary worktree edit; source branch files were not mutated. Full output is stored under `evidence/mutations/`.

| Blade | Mutation | Red assertion |
| --- | --- | --- |
| M01 | Remove helper response-header negotiation gate | `test_queue_full_terminal_ack_requires_negotiated_hub_header`, no-header case expected `busy`, got `failed`. |
| M02 | Remove Hub `delivery_ack_queue_full_v1` gate | `TestDeliveryACKV2QueueFullIgnoredWithoutHelperCapability`: unexpected `delivery_ack_confirm` for failed queue-full ACK. |
| M03 | Permit omission of `retryable` | `TestDeliveryACKV2QueueFullRequiresExplicitNonretryableShape`: omitted field was accepted. |
| M04 | Remove `queue_full` from notice reason allowlist | `TestDeliveryACKV2QueueFullAtomicallyDeadLettersAndCreatesFixedNotice`: durable sender notice missing. |
| M05 | Skip notice enqueue in terminal transaction | Same test: durable sender notice missing. The first exploratory mutation failed compilation because it left an unused local; the logged M05 result uses a compiling dead branch and fails on the business assertion. |
| M06 | Change deterministic session notice ID | Same test: expected durable notice ID is absent. |
| M07 | Change deterministic Feishu notice ID | `TestDeliveryACKV2QueueFullFeishuNoticeUsesDurableOutboundQueue`: durable sender notice missing at deterministic ID. |
| M08 | Raise existing outbound retry/dead-letter threshold from 3 to 99 | `TestMessageQueueFailRetriesThenDead`: fourth failed message was still claimable instead of dead-lettered. |
| M09 | Map generic inbox I/O failure to terminal queue-full | `test_inbox_failure_is_not_misclassified_as_queue_full`: expected `inbox_unavailable`, got `queue_full`. |
