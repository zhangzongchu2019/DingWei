# agent-icon-provenance Specification

## ADDED Requirements

### Requirement: Preserve trusted provenance when an admitted echo replaces its canonical input card

When the renderer has a canonical `input_posted` event for a delivery and later receives an admitted `user_input` event correlated to that same delivery, it MUST retain the canonical event's Hub-attested trusted provenance on the single visible turn input. The merge MUST preserve the admitted event's turn association and body while retaining the canonical provenance for display. It MUST NOT create a duplicate visible input card.

#### Scenario: Trusted Agent source remains visible after admission

- **GIVEN** a Hub-sequenced `input_posted` event with `provenance.kind` equal to `agent`, `provenance.trust_decision` equal to `trusted`, and Hub attestation
- **AND** a `user_input` event for the same delivery is admitted, has a turn identifier, and has no provenance block
- **WHEN** the renderer merges the admitted echo into the turn
- **THEN** exactly one visible input is rendered for that delivery
- **AND** that input displays the Agent provenance icon from the canonical event
- **AND** the input is associated with the admitted turn

#### Scenario: Timer Agent source remains visible after admission

- **GIVEN** a Hub-attested trusted Agent `input_posted` event whose display name is `timer`
- **AND** a matching admitted `user_input` echo has no provenance block
- **WHEN** the renderer merges the echo into its turn
- **THEN** exactly one input is visible and its timer Agent source icon remains visible

### Requirement: Preserve provenance across injection acknowledgement

When a `delivery_update` for a canonical input reports `cli_injected`, the renderer MUST move the canonical event to the timeline without dropping its provenance. A later admitted echo for that delivery MUST follow the trusted-provenance merge requirement.

#### Scenario: Icon remains through injection and admission

- **GIVEN** a trusted canonical input event and a matching `cli_injected` update with a turn identifier
- **WHEN** the injection update is processed
- **THEN** the canonical timeline input retains its Agent provenance icon
- **AND WHEN** the matching admitted echo is processed
- **THEN** the final single turn input retains that icon

### Requirement: Do not invent trusted provenance

The renderer MUST preserve provenance only from a correlated canonical event whose provenance passes the existing Hub-attestation and trusted-decision checks. If the canonical event has no provenance, has a non-trusted decision, or lacks Hub attestation, the admitted input MUST NOT display a trusted provenance icon based on the echo's text or other untrusted fields.

#### Scenario: Untrusted or absent canonical provenance stays icon-free

- **GIVEN** a canonical input event with absent, untrusted, or non-Hub-attested provenance
- **AND** a matching admitted echo with no trusted provenance
- **WHEN** the renderer merges the echo
- **THEN** the final input has no trusted provenance icon

### Requirement: Correlate only the same delivery and preserve SSE ordering

The renderer MUST carry provenance only across the existing exact delivery-correlation path for the same canonical input and admitted echo. It MUST preserve Hub SSE ordering metadata and MUST NOT transfer provenance between different deliveries.

#### Scenario: Unrelated admitted input does not inherit another source icon

- **GIVEN** two canonical input events with different delivery identifiers and distinct source provenance
- **AND** an admitted echo correlated to only one of those deliveries
- **WHEN** the renderer merges the echo
- **THEN** the admitted input inherits provenance only from its matching canonical event
- **AND** the other input remains unchanged
