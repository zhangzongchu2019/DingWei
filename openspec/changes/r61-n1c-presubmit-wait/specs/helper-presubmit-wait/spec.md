# Helper pre-submit validation wait

## ADDED Requirements

### Requirement: Transient pre-submit validation failures remain queued

The Codex helper MUST retain pre-submit validation failures as a durable FIFO-head wait or fail-closed operator hold. It MUST avoid terminal failure and thread rotation before the submission boundary.

#### Scenario: Validation transport recovers before submission

- **WHEN** a read-only pre-submit validation RPC fails transiently and later succeeds
- **THEN** the same delivery remains durably queued at the head until success
- **AND** the submission boundary is persisted before exactly one turn/start request

#### Scenario: Transient read-only validation retries

- **WHEN** supported read-only validation has a transient transport or server failure
- **THEN** the helper retries with capped backoff and does not record `failed/session_unavailable`

#### Scenario: Binding mismatch is not retried or rotated

- **WHEN** validation proves a thread identity, cwd, direct-input, authentication, or daemon identity mismatch
- **THEN** the delivery fails closed without creating or selecting another thread
- **AND** no turn/start request is sent

#### Scenario: Definitive binding mismatch remains queued for operator action

- **WHEN** validation proves a thread identity, cwd, direct-input, authentication, or daemon identity mismatch
- **THEN** the busy-wait marker remains durable and the delivery stays at the FIFO head
- **AND** the helper emits a bounded diagnostic alert and sends no terminal failed ACK
- **AND** later deliveries remain blocked until operator action and safe revalidation

### Requirement: Busy pre-submit waits survive restart without crossing submission boundary

The helper MUST keep later deliveries behind a persisted busy-wait head. On restart, a busy-wait marker with no submission marker MUST restore the same FIFO head; a submission marker MUST recover as unknown/manual and MUST NOT cause an automatic turn/start retry.

#### Scenario: Helper restarts while read-only validation is waiting

- **WHEN** the busy-wait marker exists and the submission marker is null
- **THEN** the helper restores the delivery at the FIFO head and later resumes validation

#### Scenario: Helper restarts after submission may have started

- **WHEN** the submission marker exists
- **THEN** the delivery becomes unknown/manual and no second turn/start is sent

### Requirement: Pre-submit busy state uses the existing Hub retry path

After durable busy-wait persistence, the helper MUST send the existing retryable ACK v2 busy signal and MUST NOT relax terminal failed ACK validation. The Hub MUST return that delivery to its existing queued state for a later delivery attempt.

#### Scenario: Old Hub receives a busy validation-wait ACK

- **WHEN** a compatible older Hub receives the helper's ACK v2 busy signal
- **THEN** it returns the original delivery to queued without marking it terminal
