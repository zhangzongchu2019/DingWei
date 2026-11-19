# r61-n1-busy-wait Specification

## Purpose
已确证尚未提交给模型的投递，在收件方 Codex 或 Claude 驱动忙于上一轮时保持待投、留在持久收件箱，不因等待时长被判为准入未知或失败，直到可注入或被明确终结。

## Requirements

### Requirement: Proven unsubmitted busy deliveries remain pending
The helper SHALL keep an inbound delivery pending in its durable inbox while the selected Codex or Claude driver proves that the delivery has not crossed its submission boundary and is waiting for the preceding turn to become idle. Elapsed time alone SHALL NOT convert this state to admission-unknown, reject it, or remove it from the inbox.

#### Scenario: Busy turn lasts longer than the configured busy wait limit
- **WHEN** a delivery remains in a driver-confirmed pre-submission busy wait beyond `SH_BUSY_ADMISSION_MAX_SECONDS`
- **THEN** the helper keeps the same delivery ID pending in FIFO order
- **AND** it does not emit an unknown-admission result or start a second submission

### Requirement: Submission boundary is durable and conservative
The helper SHALL atomically distinguish a persisted proven-waiting state from a delivery whose submission may have started. Once the durable state says submission may have started, timeout, process loss, or missing acknowledgement SHALL remain unknown/manual-only unless authoritative admission evidence resolves it. The helper SHALL NOT automatically replay an uncertain delivery.

#### Scenario: Process stops before the submission transition
- **WHEN** helper restart finds a durable busy-wait marker and no submission-boundary marker
- **THEN** it restores the same delivery as the next FIFO item and resumes waiting

#### Scenario: Process stops at or after the submission transition
- **WHEN** helper restart finds the durable submission-boundary marker but no authoritative admitted or terminal evidence
- **THEN** it records/retains unknown admission for manual handling and does not submit again

### Requirement: Busy waiting preserves FIFO and lifecycle identity
The helper SHALL process the earliest received eligible delivery before later deliveries for that session. A waiting delivery SHALL retain its original `delivery_id`, received ordering, ACK generation, and lifecycle/outbox chain. Progress observations SHALL describe queueing/waiting but SHALL NOT independently schedule, acknowledge, or replay a delivery.

#### Scenario: Head delivery waits for the current turn
- **WHEN** delivery A precedes B and A is proven waiting for the current turn to finish
- **THEN** B is not dispatched ahead of A
- **AND** A's original lifecycle and delivery-progress entry continue without a duplicate delivery

### Requirement: Capacity overflow is explicit (N-1b; blocked in N-1a)
The helper SHALL enforce the configured durable inbox capacity. If a new delivery cannot be persisted because the inbox is full, it SHALL return a definitive, non-retryable capacity rejection for that delivery and SHALL NOT silently drop it or classify it as an uncertain submission. The sender SHALL receive a visible rejection associated with the original delivery.

#### Scenario: Inbox reaches its configured capacity
- **WHEN** an additional delivery arrives and no capacity is available
- **THEN** the helper does not insert or submit that delivery
- **AND** the sender receives a visible `queue_full` rejection
- **AND** existing inbox rows and their order remain unchanged

### Requirement: Busy-wait recovery does not infer admission
The helper SHALL restore durable proven-waiting deliveries after restart in received order, while restoring lifecycle and ACK outbox state idempotently. A delivery without a trustworthy durable pre-submission marker SHALL NOT be inferred to be safe to resubmit.

#### Scenario: Restart restores multiple waiting deliveries
- **WHEN** restart finds multiple proven-waiting entries
- **THEN** it resumes them in persisted FIFO order using their original identifiers
- **AND** retransmitted ACK phases remain idempotent

### Requirement: Busy timeout configuration remains compatible
The helper SHALL continue to accept existing `SH_BUSY_ADMISSION_MAX_SECONDS` configuration without using it to terminate proven pre-submission busy waits. The setting MAY continue to bound post-submission admission/transport uncertainty. The helper SHALL make this distinction visible in diagnostics and documentation.

#### Scenario: Existing deployment keeps the old setting
- **WHEN** helper starts with the default or an explicitly configured `SH_BUSY_ADMISSION_MAX_SECONDS`
- **THEN** configuration parsing remains valid
- **AND** a proven busy waiter continues beyond that duration
- **AND** post-submission timeout handling retains the documented conservative behavior
