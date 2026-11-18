# Proposal: preserve trusted Agent provenance on admitted input cards

## Why

The inbound Hub event for a message from another Agent can carry Hub-attested provenance and render its Agent icon while queued. After injection, the admitted `user_input` echo is rendered as the same message in a turn group, but the helper echo has no provenance block. The renderer therefore replaces the visible event with one whose normalized provenance is untrusted/unknown, and the icon disappears. The source event already contains the trusted provenance needed for display; the renderer should preserve it when it correlates the admitted echo.

## What Changes

- Keep the canonical `input_posted` event and its Hub-attested provenance as the display source when merging the matching admitted `user_input` into a turn.
- Preserve the admitted echo's turn, timestamp, body, lifecycle, and SSE ordering information while carrying forward only the canonical provenance needed by the display.
- Apply the same rule to live event handling and history/replay merging wherever the same delivery is moved from a provisional input card into a turn group.
- Continue to fail closed when the canonical event has no Hub-attested trusted provenance. Do not infer provenance from helper text, sender labels, or delivery IDs.
- Do not change Hub behavior or event schemas.

## Capabilities

### Modified Capabilities

- `agent-icon-provenance`: preserve canonical trusted provenance when an admitted echo takes over the renderer card for the same delivery.

## Scope

This is a renderer-only change proposal. It does not change Hub provenance creation, delivery transport, or message identity contracts.

## Source

Coordinator assignment S-VIEW-AGENT-ICON-MISSING, sent by `opus50tc2k.zzc` on 2026-11-16, based on the owner's current-page screenshot. The reported renderer candidate is `4a8d8cc1` (`a0e28049`).
