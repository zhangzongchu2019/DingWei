# Compatibility probe notes

The four JSON files are raw output from `SessionHelper.accept_delivery` with a full one-item local inbox and a fake websocket that records the actual helper ACK payload. The helper code was imported from these exact repository snapshots:

- old helper: `206d41b26dab646c0e50a111eed5a45ccf7c72f6` (reported version 0.2.3.40);
- new helper: source anchor `c0ec52db044d92a539a5d7010e597d7338787fc1` (version string intentionally remains 0.2.3.40).

Hub capability was modeled by whether the exact response header was present on the connection. The old Hub's absent-header case and the new Hub's present-header case were exercised through the helper method; these four are not separate live Hub processes. Actual new Hub websocket handshake/header and terminal ACK acceptance/rejection are covered by `TestDeliveryACKV2QueueFullRequiresNegotiationAndCreatesDurableNotice`, `TestDeliveryACKV2QueueFullIgnoredWithoutHelperCapability`, and `TestNewHubOldHelperQueueFullKeepsRetryableBusyPath` in the full m8 log.

The ACK JSON contains synthetic sender/session identifiers only; no credentials or real endpoint values were used.
