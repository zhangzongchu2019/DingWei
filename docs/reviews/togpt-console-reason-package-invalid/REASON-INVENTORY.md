# Helper self-update reason inventory

This is the closed, non-sensitive reason vocabulary for
`S-CONSOLE-REASON-PACKAGE-INVALID`. The Hub persists only the listed
wire-reachable and compatibility values; it maps every other receipt value to
`unknown`. Console maps its safe projection to fixed text and never renders a
raw helper exception string.

## Defined but not wire-emitted

`self_update.py` defines `confirmation_invalid` and
`confirmation_mismatch`. They occur only during confirmation processing, whose
exception path is deliberately suppressed, so neither is accepted as a helper
failure-receipt reason.

## Current wire-reachable helper reasons

`self_update.py`: `download_failed`, `download_limit`, `lock_changed`,
`manual_required`, `offer_expired`, `offer_invalid`, `offer_mismatch`,
`origin_denied`, `origin_invalid`, `package_invalid`, `platform_unsupported`,
`redirect_denied`, `release_mismatch`, `sha256_mismatch`, `slot_exists`,
`state_invalid`, `target_mismatch`, `update_busy`, `version_invalid`, and
`version_not_new`.

`app.py`: `hub_unsupported`, `update_failed`, and `busy`.

The stable launcher rollback path: `candidate_failed`, `health_timeout`, and
`slot_invalid`.

`package_release.PackageError` details, including `manifest_invalid`, are not
wire values: helper normalization emits only `package_invalid`.

## Compatibility and Hub-local durable reasons

Existing trusted helper jobs retain `sha_mismatch`, `install_failed`,
`rollback`, `expired`, `start_failed`, and `rolled_back`. Hub recovery retains
`agent_inactive`, `owner_changed`, `owner_inactive`, and
`release_unavailable`.

## Regression boundary

The isolated E2E test constructs a hash-matching new-layout artifact with a
`layout-v2` member. The old verifier rejects it as `manifest_invalid`; helper
normalizes it to `package_invalid`, the authenticated matching job persists it,
and the Console API projects it. The same test asserts no slot is created and
the factory remains running, so rejection cannot restart or activate a
candidate.
