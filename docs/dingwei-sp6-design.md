# DingWei SP6 design v0.1 — Agent runtime and messaging

Status: draft. Contract: `specs/sp6/spec.md`.

## 1. Boundary and authority

SP6 is a delivery envelope, not a domain. `SP6-AGT` owns runtime order, instance, resource lease, health and emergency-session lifecycle. `SP6-MSG` owns session endpoints, canonical envelopes, routes, deliveries, ACKs and channel receipts. GOV/KNO/WRK/COL remain foreign authorities consumed through ports.

| Concern | Authority | SP6 behavior |
|---|---|---|
| approval and emergency capability effect | GOV/SP8 | consume current decision; unknown/stale/unavailable rejects |
| principal and channel/session binding | IDN/SP1 | consume verified AccessCtx/SessionAuthRef; never trust body claims |
| task and dispatch snapshot | WRK/SP2 + KNO/SP3 | display/consume immutable refs; do not redefine |
| posts and attachments | COL/SP5 | transport references; do not own content authorization |
| controlled transactions | PLT/SP7a | all persistent operations use repository ports |

## 2. AGT package

`CreateRuntimeOrder` accepts a closed schema: project/role, runtime shape, controlled resource IDs, requested capabilities and expiry. Unknown fields fail. Credentials, arbitrary URLs, environment-variable material, attachments and unscanned free text have no representation. Principal comes from AccessCtx, never request JSON.

`ActivateInstance` is a generation-fenced state machine: `pending → provisioning → active | reconcile | failed | revoked`. A partial external effect never guesses success. It records the observed boundary, enters reconcile and preserves the same request ID. Late receipts from an older generation are fenced.

`ReportHealth` carries instance generation, sample ID, observed timestamp and bounded watermarks. Older samples may be audited but cannot overwrite newer state. Health distinguishes live, ready, degraded and safe mode; dependency unknown stays unknown.

`EmergencyAction` is a local-sidecar capability, not a second policy engine. Before effect it verifies local principal, allowlisted operation, current SP8 decision, non-expanding capability effect, target generation and redaction/audit availability. Any unknown rejects the effect. The sidecar binds loopback only, uses a route independent from the platform application, and appends a local tamper-evident audit record. Platform-down may still work; policy/redaction-down may not.

This explicitly closes the SP8 §24.3a consumer-side obligation: `docs/dingwei-sp8-design.md` remains the authority for emergency admission and monotonic tightening, while `AGT-CAP-EmergencyAction` is the only SP6 execution seam. A PASS on SP8 never substitutes for this enforcement. Absence, mismatch or unavailability of the SP8 decision is fail-closed before effect.

## 3. MSG package

At ingress, the adapter converts channel input to a canonical envelope. Authenticated connection metadata supplies sender identity; user-controlled `from`, tenant, principal or session claims are discarded. Attachments are scoped references with digest, never inline authority.

Routing resolves a typed selector before reading payload. It produces a delivery ledger entry with enterprise, project scope, endpoint generation, delivery ID and retry state. Cross-enterprise selectors fail unless a governed capability explicitly permits them. Routing cannot infer authorization from message text.

ACK records receipt at a particular endpoint generation. It does not mean the business command ran. Business outcome is a separate domain result. Duplicate envelopes and deliveries return the existing outcome; late ACK cannot revive a closed/revoked generation. Ordering is only guaranteed within the declared selector/stream scope.

Channel adapters absorb format, size, fragmentation, rich-text, attachment, receipt and retry differences. Unsupported capability returns explicit degradation; it never silently truncates or drops security-relevant material.

## 4. UI and operational composition

SP6 supplies adapters for Agent roster/work surface, direct Agent interaction and channel entry. Typed pending queues remain GOV, knowledge search remains KNO, task state remains WRK, and content/attachments remain COL. UI buttons call those ports and render their decision references; UI state is not authority.

## 5. Interfaces

| ID | Input/output | Authorization | Idempotency/generation | Failure/audit |
|---|---|---|---|---|
| `AGT-CMD-CreateRuntimeOrder` | scoped order → order ref | AccessCtx principal + governed resources | request/order generation | closed-schema reject; audit metadata |
| `AGT-CMD-ActivateInstance` | order + lease + receipt → state | provision service | request/instance generation | partial→reconcile; fenced late receipt |
| `AGT-CMD-ReportHealth` | instance sample → health snapshot | session principal | sample ID + instance generation | stale ignored and audited |
| `AGT-CAP-EmergencyAction` | allowlisted effect → result ref | local emergency principal + SP8 decision | request/target generation | unknown rejects before effect; local append-only audit |
| `MSG-CMD-AcceptEnvelope` | raw input → canonical envelope | SessionAuthRef | envelope ID | forged claims removed/rejected |
| `MSG-CMD-RouteDelivery` | envelope ref + selector → delivery | service + AccessCtx | delivery ID + endpoint generation | scope reject; route audit |
| `MSG-CMD-AcknowledgeDelivery` | delivery/generation/receipt → ACK | recipient session principal | delivery ID + generation | late ACK fenced |
| `MSG-QRY-OnlineSessions` | scoped query → bounded snapshot | AccessCtx | read-only | no cross-enterprise existence leak |

## 6. Coverage

<!-- COVERAGE-SP6-BEGIN -->

| Source obligation | Deliverable |
|---|---|
| `BOUNDARY:SP6` | `D-SP6-01` `D-SP6-11` |
| `ARCH17:§25.4/§25.8 交互入口` | `D-SP6-07` `D-SP6-08` `D-SP6-10` |
| `ARCH17:§25.5–§25.6 Agent 面` | `D-SP6-02` `D-SP6-03` `D-SP6-04` `D-SP6-11` |
| `ARCH17:§26 Agent 管理` | `D-SP6-02` `D-SP6-03` `D-SP6-04` `D-SP6-06` |
| `ARCH17:§27 渠道接入` | `D-SP6-07` `D-SP6-08` `D-SP6-09` `D-SP6-10` |
| `ARCH17:F.1 Agent 承接` | `D-SP6-02` `D-SP6-11` |
| `IF:AGT-CMD-CreateRuntimeOrder` | `D-SP6-02` |
| `IF:AGT-CMD-ActivateInstance` | `D-SP6-03` |
| `IF:AGT-CMD-ReportHealth` | `D-SP6-04` |
| `IF:AGT-CAP-EmergencyAction` | `D-SP6-05` `D-SP6-06` |
| `IF:MSG-CMD-AcceptEnvelope` | `D-SP6-07` `D-SP6-10` |
| `IF:MSG-CMD-RouteDelivery` | `D-SP6-08` |
| `IF:MSG-CMD-AcknowledgeDelivery` | `D-SP6-09` |
| `IF:MSG-QRY-OnlineSessions` | `D-SP6-08` `D-SP6-11` |
| `E:E.1#31` | `D-SP6-12` |
| `E:E.1#92` | `D-SP6-12` |
| `E:E.1#93` | `D-SP6-12` |
| `E:E.1#94` | `D-SP6-12` |
| `E:E.1#95` | `D-SP6-12` |
| `E:E.1#96` | `D-SP6-12` |
| `E:E.1#97` | `D-SP6-12` |
| `E:E.1#100` | `D-SP6-12` |
| `E:E.1#108` | `D-SP6-12` |
| `E:E.1#109` | `D-SP6-12` |
| `E:E.1#110` | `D-SP6-12` |
| `E:E.1#111` | `D-SP6-12` |
| `E:E.1#112` | `D-SP6-12` |
| `E:E.1#113` | `D-SP6-12` |
| `E:E.1#114` | `D-SP6-12` |
| `E:E.1#124` | `D-SP6-12` |
| `E:E.1#133` | `D-SP6-12` |
| `E:E.3#15` | `D-SP6-12` |
| `E:E.3#24` | `D-SP6-12` |
| `E:E.3#25` | `D-SP6-12` |
| `E:E.3#26` | `D-SP6-12` |
| `E:E.3#27` | `D-SP6-12` |
| `E:E.3#29` | `D-SP6-12` |
| `E:E.3#30` | `D-SP6-12` |
| `E:E.3#31` | `D-SP6-12` |
| `E:E.3#41` | `D-SP6-12` |
| `E:E.3#43` | `D-SP6-12` |
| `E:E.4#27` | `D-SP6-12` |
| `E:E.4#45` | `D-SP6-12` |
| `E:E.4#47` | `D-SP6-12` |
| `E:E.4#51` | `D-SP6-12` |
| `E:E.4#62` | `D-SP6-12` |

<!-- COVERAGE-SP6-END -->

## 7. Conformance and evidence

Delivery verification must include closed-schema carrier mutations; forged sender and cross-enterprise selector cases; replay, duplicate and late-generation cases; platform-down emergency positive case; policy/redaction-down and expanding-effect negative cases; loopback-only network evidence; bounded latency/retry/queue metrics with source and window. Missing fixtures or unavailable external dependencies yield INCONCLUSIVE, never green.
