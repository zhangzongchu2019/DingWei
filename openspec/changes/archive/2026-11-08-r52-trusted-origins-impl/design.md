# Design

## Trusted origin configuration and decision

`DW_TRUSTED_ORIGINS` is an optional comma-separated list of exact HTTPS origins. Reject malformed entries, wildcards, missing or noncanonical ports, credentials, paths, query/fragment, and normalized duplicates at startup. Normalize scheme and host to lowercase, retain the explicit port, and compare the complete `(scheme, host, port)` tuple.

When the list is empty, each existing handler uses its current comparison path without parsing or normalizing legacy values. When non-empty, the existing canonical `PublicOrigin` remains accepted and configured trusted origins are added. Existing endpoint host binding, method, session, authorization, JSON, and Fetch Metadata checks remain in force. The authenticated `/view` WebSocket follows the same view gate. SSE validates a supplied Origin before stream headers; requests without Origin retain existing behavior.

## Browser file URL flow

The upload capability issuer chooses a browser upload URL only from a request Origin that exactly equals the canonical `PublicOrigin` or matches a configured trusted origin. Ambiguous or untrusted Origins use the existing file-service base. This selection does not change the file-service response URL or the canonical URL stored in messages/specifications.

View Service receives the canonical public origin through read-only configuration. Its renderer may map canonical `/f/` links to the current page origin for local display. Before sending, it restores those links to the canonical origin. Keep the generated renderer sections and their parity fixtures synchronized.

## Validation

- Unit and handler tests cover parser rejection, canonical and exact trusted-origin acceptance, different-port and HTTP rejection, and empty-list compatibility.
- Browser regression covers 443 and 50443 upload, draft thumbnail, canonical stored body, and post-send rendering, with negative controls for omitted normalization and location-only rendering.
- The m8 package race suite runs in five contiguous, serial shards, each with `-race -count=3`.
