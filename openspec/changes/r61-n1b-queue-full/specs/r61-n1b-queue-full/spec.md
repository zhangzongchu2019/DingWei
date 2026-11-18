# Requirements

## ADDED Requirements

### Requirement: Queue capacity rejection is a negotiated terminal ACK
Hub and helper MUST use the `queue_full` terminal rejection only when the current helper connection declares `delivery_ack_queue_full_v1` and the Hub handshake response advertises `X-DW-Queue-Full-ACK: queue-full/v1`. The ACK MUST identify the exact delivery and ACK generation and MUST be `failed` with reason `queue_full` and `retryable=false`.

#### Scenario: Negotiated helper cannot persist delivery because inbox is full
- **WHEN** durable inbox acceptance proves the new delivery was not stored and capacity is exhausted
- **THEN** helper sends one constrained terminal ACK for that delivery
- **AND** Hub records terminal rejection and confirms the ACK

#### Scenario: Capability is absent or Hub is old
- **WHEN** either capability signal is missing
- **THEN** helper uses the existing retryable busy behavior and Hub does not interpret the new semantic

### Requirement: Queue-full terminal is durable and visible to sender
Hub MUST commit the terminal delivery state and a deduplicated durable sender-notice outbox item in one transaction. The notice MUST identify the original delivery, say it was not submitted because the target inbox was full, and MUST NOT include the original body or helper-supplied free-form text.

#### Scenario: Duplicate ACK or ACK confirmation loss
- **WHEN** Hub receives a duplicate queue-full ACK or the helper misses the confirmation and retries it
- **THEN** the original delivery remains terminal and the sender receives at most one durable notice

### Requirement: Queue-full is limited to proven pre-persistence overflow
Hub and helper MUST NOT classify busy waiting, possible submission, admission unknown, or any persisted delivery as queue-full rejection.

#### Scenario: Delivery may already have been submitted
- **WHEN** the delivery has a durable inbox row or a submission may have started
- **THEN** the queue-full terminal path is unavailable and the existing lifecycle or unknown/manual rules apply
