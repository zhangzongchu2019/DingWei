# DingWei SP7 design v0.1 — controlled platform mechanism

Status: draft. Contract: `specs/sp7/spec.md`.

## 1. Boundary

SP7-PLT owns shared controlled mechanisms, not business semantics. SP7a owns enterprise registry projection, repository/transaction ports, RLS conformance, migration runner, audit, health/metrics and reconcile mechanisms. SP7b remains with each business-domain owner for schema, policy, backfill, consistency query, cutover and rollback.

The historical `docs/SP7A-DESIGN.md` correctly identifies typed context and handle containment, but it predates the v0.2 enterprise-per-database topology and PLT interfaces. This design supersedes it as the SP7 contract target without rewriting its history.

## 2. Repository and transaction ports

The platform registry maps a stable enterprise ID to an opaque database key. Callers never receive DSN or pool handles. Selection is performed inside the repository after verifying service identity, AccessCtx and DataScopeGrant.

`WithAccess` validates context/grant/generation, selects the enterprise pool, begins a transaction, applies access settings as its first statement, invokes a domain callback and commits domain changes with outbox/audit. Any failure rolls back. Application predicates remain mandatory; RLS is defense in depth.

`WithSystem` is limited to enumerated platform maintenance that has no enterprise-person read path. `WithMigration` requires an immutable artifact digest, migration approval, enterprise target, from/to version and schema generation. Neither can be obtained by passing a zero/empty access context.

Raw DB/pool/transaction handles do not leave the package through returns, exported fields, aliases, anonymous embedding, interfaces or callbacks. A Go AST detector and mutation corpus enforce this boundary.

## 3. RLS and role conformance

Every person-scoped table has an explicit owner principal predicate. RLS provides both `USING` and `WITH CHECK`; missing session settings read zero rows and writes fail. Runtime roles are non-superuser, `NOBYPASSRLS`, non-owner and have no DDL. The verifier reports each role/owner/policy/FORCE/WITH CHECK fact. Missing catalog access, object or fixture is INCONCLUSIVE.

PEP rejection and query isolation are separate evidence: denial occurs before resource resolution with query count zero; semantic denial is not disguised as a successful empty result; existing and absent forbidden resources are indistinguishable; audit is minimized.

Backups, replicas, replication slots and privileged operator paths are outside RLS. They require distinct encryption, approval, access logging and immutable audit; passing application RLS tests does not close those paths.

## 4. Migration runner and SP7b handoff

The runner verifies grant, artifact digest and schema generation, records start, creates a recovery point, runs one enterprise in an isolated migration transaction, then runs the domain consistency query. Failure stops later enterprises. Rollout is small batch, verify, expand; an irreversible step requires backup and recovery rehearsal evidence before approval.

Every domain SP7b handoff contains:

| Object | Input | Expected | Evidence | Gate |
|---|---|---|---|---|
| domain schema/policy/backfill/cutover unit | immutable artifact + from/to + enterprise scope | schema and semantic invariant | runner record + consistency query + rollback/recovery evidence | domain owner + migration/security verification |

SP7a validates the envelope but does not invent the domain object or expected invariant. Board migration remains blocked until its concurrency conformance suite exists.

## 5. Other PLT mechanisms

AppendAudit accepts a canonical record and produces an idempotent append receipt. It never stores credentials or raw enterprise bodies in the platform audit stream. Protection failure follows the protected action's policy; it cannot merely suppress the record while allowing a protected effect.

HealthSnapshot and MetricsWindow are bounded, scoped and redacted. Unknown dependencies stay unknown. ReconcileEffect stores request/result references, observation and generation; it never guesses success when the external effect is uncertain.

## 6. Interfaces

| ID | Contract |
|---|---|
| `PLT-CMD-RunInAccessTx` | verified AccessCtx/grant → first-statement settings → domain callback → atomic state/outbox/audit |
| `PLT-CMD-RunMigration` | approval + artifact + enterprise + generation → isolated runner record/recovery point/result |
| `PLT-QRY-RLSConformance` | scoped catalog read → explicit role/owner/policy/FORCE/WITH CHECK facts or INCONCLUSIVE |
| `PLT-CMD-AppendAudit` | canonical minimized record → idempotent append receipt |
| `PLT-QRY-HealthSnapshot` | authorized component/dependency refs → live/ready/degraded/safe-mode/unknown |
| `PLT-QRY-MetricsWindow` | authorized metric/window/scope → bounded aggregates without high-cardinality bodies |
| `PLT-CMD-ReconcileEffect` | owner request/result refs + observation/generation → reconciled or still unknown |

## 7. Coverage

<!-- COVERAGE-SP7-BEGIN -->

| Source obligation | Deliverable |
|---|---|
| SP7a mechanism and SP7b domain handoff boundary | `D-SP7-01` |
| architecture §17 owner PLT rows | `D-SP7-02` `D-SP7-03` `D-SP7-04` `D-SP7-05` `D-SP7-06` |
| architecture §17 owner PLT.Security rows | `D-SP7-07` `D-SP7-08` `D-SP7-10` |
| architecture stable interfaces `PLT-*` | `D-SP7-03` `D-SP7-05` `D-SP7-06` `D-SP7-07` `D-SP7-08` `D-SP7-09` |
| SpecDoc §22.2/C.3a SP7a mechanism | `D-SP7-02` `D-SP7-03` `D-SP7-04` `D-SP7-05` `D-SP7-06` |
| per-domain SP7b migration/cutover | `D-SP7-11` |
| raw-handle and repository boundary | `D-SP7-10` |
| Appendix E rows owner SP7a/SP7b | `D-SP7-12` |

<!-- COVERAGE-SP7-END -->

## 8. Delivery evidence

Required evidence includes AST boundary selftests, separate PEP/query-spy and semantic-denial tests, PG role/RLS mutation matrix, wrong-enterprise/forged-principal/missing-context cases, migration failure-stop and recovery evidence, per-domain consistency queries, and bounded health/metrics/reconcile outputs. Missing fixture/object/catalog access is INCONCLUSIVE, never PASS.
