# SP6 platform capability contract — AGT and MSG packages

Status: draft for rotating full-document review.

## 0. Frozen input and package boundary

SP6 is one delivery envelope with two independently owned domain packages derived from architecture §17:

- `SP6-AGT`: architecture rows owned by `AGT`, including Agent runtime orders, instances, resources, health, sidecar and emergency execution.
- `SP6-MSG`: architecture rows owned by `MSG`, including canonical envelopes, online sessions, addressing, delivery, ACK and channel normalization.

This contract does not reclaim GOV/KNO/COL/WRK rows that happen to appear in SpecDoc §25–§27. Those domains expose ports; SP6 composes them without taking ownership. Existing SP8 remains the authority for emergency authorization. SP6 owns enforcement at `AGT-CAP-EmergencyAction`.

Frozen baseline: `design/v0.2` at `15bc4a4fe60f95b8236a94f8d650cbe34232ec78`.

| Input | sha256 | Purpose |
|---|---|---|
| `docs/dingwei-platform-specdoc.md` | `8889a2e8fc73ffccee17555362a4e9be8da9f700c15273b4092c3a9c0b38baff` | §25–§27 and Appendix E ownership |
| `docs/dingwei-platform-architecture.md` | `94b067f8de0c38e81fc3ffb6594b695357286b794223e5d2ae7c78a03ade8d2c` | AGT/MSG rows, interfaces and runtime topology |
| `docs/dingwei-prototype.html` | `f2995730ca986ae38993f62391312982e7ed5026dab990c78ff29a6bb00ef9cb` | UI entries; existence is not implementation |
| `docs/dingwei-sp8-design.md` | `4f5e4e06d29d4a9fffcdb46f413fbc37bcfd615e9af9b04215e0a6c20205a98a` | emergency authorization authority consumed by SP6 |

## 1. Required design surfaces

1. System integration: AGT and MSG ports, adapters, outbox seams and UI composition.
2. Runtime constraints: identity provenance, generation fences, fail-closed emergency path, bounded retry and degradation.
3. Expected data: workload and reliability measurements with source, window and `unknown` handling.
4. Environment: cloud/IDE/local trust tiers, controlled resource references, platform-independent sidecar emergency path.
5. Design constraints: no new domain ownership, no credential material in orders, no authorization from envelope body.
6. Input provenance: immutable baseline and content digests above.

## 2. Deliverables

| ID | Package | Required content |
|---|---|---|
| `D-SP6-01` | shared | scope, AGT/MSG ownership, consumed foreign ports and fail-closed boundaries |
| `D-SP6-02` | AGT | runtime order closed schema, resource references, trust tier and approval derivation |
| `D-SP6-03` | AGT | instance activation/reconcile, generation fence, partial-failure states and audit |
| `D-SP6-04` | AGT | health/watermark model, stale-sample handling, live/ready/degraded/safe-mode semantics |
| `D-SP6-05` | AGT | emergency action enforcing SP8 capability-effect decision before execution |
| `D-SP6-06` | AGT | sidecar independence, localhost-only bind, local append-only audit and redaction failure behavior |
| `D-SP6-07` | MSG | canonical envelope construction; sender claims removed and SessionAuthRef used |
| `D-SP6-08` | MSG | selector resolution, tenant/project scoping, delivery ledger and generation fence |
| `D-SP6-09` | MSG | ACK semantics, retry/dedupe/order handling and distinction from business success |
| `D-SP6-10` | MSG | channel normalization, capability degradation, fragmentation and attachment references |
| `D-SP6-11` | shared | §25 UI adapter composition without duplicating GOV/KNO/WRK/COL authority |
| `D-SP6-12` | shared | interface schemas, conformance matrix, expected data and delivery-verification handoff |

The design body must contain one `COVERAGE-SP6-BEGIN/END` table mapping every derived architecture §17 AGT/MSG row, every AGT/MSG interface and every SP6-owned Appendix E row to a real deliverable ID.

## 3. Hard invariants

- Agent provenance never substitutes for principal authorization.
- Runtime orders use a closed schema; no credential, arbitrary URL, environment variable, attachment or unscanned free text can carry secrets.
- Emergency execution requires a current SP8 authorization result and a non-expanding capability effect. Unknown, stale or unavailable policy means no execution.
- Sidecar emergency routing remains available when the platform application is down, but cannot become a general bypass.
- Envelope body cannot authorize routing or assert sender identity. MSG uses authenticated connection metadata and scoped selectors.
- ACK means receipt only. Business execution has a separate result/effect record.
- Old generation ACK/health/activation results are fenced and cannot resurrect revoked state.
- Cross-enterprise online-session discovery and delivery are denied without an explicit governed capability.

## 4. Acceptance

<!-- ACCEPTANCE-SP6-BEGIN -->

| ID | Scope | Criterion | Cannot prove |
|---|---|---|---|
| `AC-SP6-01` | architecture §17 rows owner AGT/MSG | expected and implemented coverage sets equal; unknown set empty | semantic quality |
| `AC-SP6-02` | architecture interfaces `AGT-*` and `MSG-*` | each stable ID has schema, auth, idempotency/generation, tx/effect, errors and audit | implementation correctness |
| `AC-SP6-03` | runtime order | forbidden carrier fields and unknown fields fail; controlled resource IDs only | external pool correctness |
| `AC-SP6-04` | emergency action | platform-down positive path plus policy-down, expansion, stale grant and redaction-down negative paths | real incident judgment |
| `AC-SP6-05` | messaging | forged sender, cross-tenant selector, replay, late ACK and duplicate envelope negative cases | channel provider SLA |
| `AC-SP6-06` | UI adapters | foreign-domain actions call owner ports and do not duplicate authority | usability |
| `AC-SP6-07` | evidence/metrics | bounded windows, provenance and explicit unknown; no hand-written current counts | production capacity |
| `AC-SP6-08` | review | alternating one-reviewer full-document rounds, all findings exposed, final PASS, no more than six rounds | reviewer expertise |

<!-- ACCEPTANCE-SP6-END -->

## 5. Review protocol

Each round selects exactly one of `opus50tczc2000` and `qwen38max`, alternating between rounds. The reviewer reads the entire contract/body/checker set and returns one complete findings list plus `PASS`, `NO`, or `INCONCLUSIVE`. The author addresses a whole round before the next full review. No single-detail ping-pong. At most six rounds.
