# trusted-origin-policy Specification

## Purpose
规定 Console 与 View 在规范公网地址之外，只接受显式配置的精确 HTTPS 来源（如反向代理提供的额外端口）：用于浏览器写接口的 Origin 校验和上传票据地址选择；持久化内容与对外链接始终使用规范公网地址，清单为空时保持原有行为。来源：r52 变更 r52-trusted-origins-impl（2026-11-06 上线，0.2.3.27）。

## Requirements

### Requirement: Trusted-origin configuration uses exact HTTPS origins

The service MUST accept additional browser sources only from explicitly configured HTTPS origins. It MUST compare normalized scheme and host with the exact explicit port. Configuration MUST reject malformed, wildcard, non-HTTPS, noncanonical-port, credential-bearing, path-bearing, query-bearing, fragment-bearing, and normalized duplicate entries.

#### Scenario: Invalid or duplicate configuration is rejected

- **WHEN** `DW_TRUSTED_ORIGINS` contains a malformed, wildcard, HTTP, noncanonical-port, credential-bearing, path, query, fragment, or normalized duplicate entry
- **THEN** startup MUST reject the configuration

### Requirement: Trusted origins extend existing browser source checks

The service MUST accept the canonical `PublicOrigin` and exact configured trusted origins when all endpoint-specific checks pass. It MUST keep existing host binding, Fetch Metadata, method, authentication, and authorization checks. The authenticated View WebSocket MUST use this policy. An SSE request with an untrusted supplied Origin MUST be rejected before stream headers are written.

#### Scenario: Exact configured HTTPS origin is accepted

- **WHEN** an authenticated browser request uses the canonical PublicOrigin or an exact normalized origin in `DW_TRUSTED_ORIGINS`
- **AND** the request satisfies the endpoint's existing checks
- **THEN** the source-origin check MUST allow the request

#### Scenario: Different port or HTTP origin is rejected

- **WHEN** a request uses an origin with a different port or HTTP scheme
- **THEN** the source-origin check MUST reject it

#### Scenario: Empty list preserves legacy behavior

- **WHEN** `DW_TRUSTED_ORIGINS` is empty
- **THEN** each endpoint MUST produce the same source decision as its pre-change predicate, including its previous handling of an absent Origin

### Requirement: Browser file links remain canonical outside local display

The Hub MUST select a browser upload-ticket URL from the request Origin only when that Origin exactly matches `PublicOrigin` or a configured trusted origin. The file service MUST continue returning canonical public file URLs, and persisted message or specification content MUST use those canonical URLs. The View renderer MAY map canonical `/f/` links to the current page origin for display, but MUST restore canonical URLs before sending content.

#### Scenario: Trusted browser origin receives an upload ticket

- **WHEN** a browser requests an upload ticket using the canonical or an explicitly trusted HTTPS origin
- **THEN** the ticket URL MUST use that request origin while the resulting file URL remains canonical

#### Scenario: Displayed file URL is normalized before sending

- **WHEN** the View renderer displays a canonical file URL through the current trusted page origin and the user sends the content
- **THEN** the sent or persisted content MUST contain the canonical file URL
