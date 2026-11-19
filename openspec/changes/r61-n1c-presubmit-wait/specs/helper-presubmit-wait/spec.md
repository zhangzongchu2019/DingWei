# Helper pre-submit validation wait

## ADDED Requirements

### Requirement: Transient pre-submit validation failures remain queued

The Codex helper MUST persist classified transient pre-submit failures at the FIFO head for retry. A definitive authentication rejection from the delivery's first direct initialize before submission MUST use the non-retryable failed path. Authentication rejection during replacement startup, read/resume authentication and binding mismatches MUST remain fail-closed operator holds. Unknown submission state MUST NOT be retried or replayed.

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

#### Scenario: Initial initialize authentication rejection is a non-retryable failure

- **WHEN** the current delivery has no submission marker and its initial Codex initialize handshake returns a definitive authentication rejection
- **THEN** the helper records the existing non-retryable `failed/session_unavailable` result
- **AND** it sends no thread/start or turn/start for that delivery
- **AND** the rejection is not retained as a busy-wait or operator-held FIFO item

#### Scenario: Read or resume authentication mismatch remains an operator hold

- **WHEN** thread/read or thread/resume returns an authentication or binding rejection before submission
- **THEN** the helper keeps the durable busy marker and the same delivery at the FIFO head
- **AND** it emits an operator alert without a terminal failed ACK
- **AND** it does not rotate or submit a different thread

#### Scenario: Replacement initialize authentication rejection stays quarantined

- **WHEN** a transient pre-submit failure has already entered recovery and replacement `start()` receives a definitive initialize authentication rejection
- **THEN** recovery does not retry the replacement or send a terminal failed ACK
- **AND** it keeps the same delivery quarantined at the FIFO head and emits an operator alert

#### Scenario: Recovery does not retry an unknown submission or other definitive refusal

- **WHEN** replacement startup fails with identity, daemon, binding, or another definitive refusal
- **OR** the current delivery has a non-null submission marker
- **THEN** recovery remains quarantined and emits an operator alert
- **AND** it does not automatically retry startup or send another turn/start
- **AND** later FIFO items remain blocked

#### Scenario: Non-authentication binding mismatch is not retried or rotated

- **WHEN** validation proves a thread identity, cwd, direct-input, or daemon identity mismatch
- **THEN** the delivery fails closed without creating or selecting another thread
- **AND** no turn/start request is sent

#### Scenario: Definitive read/resume binding mismatch remains queued for operator action

- **WHEN** thread/read or thread/resume proves an authentication, thread identity, cwd, direct-input, or daemon identity mismatch
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
