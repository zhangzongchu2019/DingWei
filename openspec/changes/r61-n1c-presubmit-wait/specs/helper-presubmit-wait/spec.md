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

#### Scenario: Recovery startup retries repeated transient initialization failures

- **WHEN** replacement CLI startup fails with a classified transient initialize transport or server error
- **AND** the current delivery has no durable `submission_may_have_started_at` marker
- **THEN** recovery retries startup with capped backoff while retaining the current FIFO head
- **AND** it does not send a terminal failed ACK or submit a later FIFO item before recovery
- **AND** after recovery the original head and later items retain FIFO order

#### Scenario: Recovery does not retry an unknown submission or definitive refusal

- **WHEN** replacement startup fails with authentication, identity, daemon, binding, or other definitive refusal
- **OR** the current delivery has a non-null submission marker
- **THEN** recovery remains quarantined and emits an operator alert
- **AND** it does not automatically retry startup or send another turn/start
- **AND** later FIFO items remain blocked

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

### Requirement: Pre-submit busy state preserves the Hub receipt boundary

After durable busy-wait persistence, the helper MUST send the existing retryable ACK v2 busy signal and MUST NOT relax terminal failed ACK validation. Hub behavior depends on whether it has persisted `DeliveredAt` for the delivery.

#### Scenario: Busy arrives before Hub records delivery

- **WHEN** the Hub receives the busy ACK before `DeliveredAt` is persisted
- **THEN** the Hub uses its existing busy-to-queued retry path
- **AND** it does not mark the delivery terminal

#### Scenario: Busy arrives after helper has confirmed queue receipt

- **WHEN** the Hub receives the busy ACK after `DeliveredAt` is non-null
- **THEN** the Hub keeps the delivery in `delivered` and treats busy as informational
- **AND** it does not reset the receipt boundary or schedule a second delivery
- **AND** the helper's durable FIFO keeps the same delivery at its head until read-only validation recovers
- **AND** after recovery the same delivery reaches exactly one `turn/start` with no loss
