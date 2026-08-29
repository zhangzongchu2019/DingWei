# SP7 platform mechanism contract — SP7a base and SP7b boundary

Status: draft for rotating full-document review.

## 0. Scope and frozen input

`SP7-PLT` is the v0.2 domain package derived from architecture §17 rows owned by `PLT` or `PLT.Security`. It contains:

- SP7a: enterprise registry projection, controlled repository ports, access/system/migration transactions, RLS conformance, migration runner, audit/health/metrics/reconcile mechanisms.
- SP7b boundary: each business-domain owner supplies its own schema, policy, backfill, dual-read, cutover and rollback contract. SP7a orchestrates and verifies; it does not invent domain schema.

Frozen baseline: `design/v0.2` at `15bc4a4fe60f95b8236a94f8d650cbe34232ec78`.

| Input | sha256 | Purpose |
|---|---|---|
| `docs/dingwei-platform-specdoc.md` | `8889a2e8fc73ffccee17555362a4e9be8da9f700c15273b4092c3a9c0b38baff` | §22.2/C.3a and Appendix E |
| `docs/dingwei-platform-architecture.md` | `94b067f8de0c38e81fc3ffb6594b695357286b794223e5d2ae7c78a03ade8d2c` | PLT rows, data topology, interfaces and migration stages |
| `docs/SP7A-DESIGN.md` | `1aa0a616d60864d511e81511ffc1a7bf3e13c7c21cd2d2c137e05af4b6bd9eb6` | historical short design; gap input, not final authority |

## 1. Required design surfaces

1. System integration: repository ports and their consumers, enterprise registry, audit/outbox and verifier seams.
2. Runtime constraints: transaction first statement, pool selection, RLS/app predicates, generation and fail-closed behavior.
3. Expected data: pool/tx/migration/audit/health/reconcile measurements with bounded windows and unknown states.
4. Environment: PG fixture, role matrix, backup/replica boundary, migration grants and no production writes during design.
5. Design constraints: raw DB/Pool/Tx handles do not escape, no caller-built tenant predicate, no distributed transaction claim.
6. Input provenance: immutable baseline and digests above.

## 2. Deliverables

| ID | Layer | Required content |
|---|---|---|
| `D-SP7-01` | SP7a | scope, PLT/PLT.Security ownership and SP7b handoff boundary |
| `D-SP7-02` | SP7a | enterprise registry projection and opaque database-key resolution |
| `D-SP7-03` | SP7a | `WithAccess`, `WithSystem`, `WithMigration` typed transaction contracts |
| `D-SP7-04` | SP7a | pool selection, first-statement session settings, explicit predicates and RLS defense in depth |
| `D-SP7-05` | SP7a | role/owner/FORCE/USING/WITH CHECK conformance verifier and INCONCLUSIVE behavior |
| `D-SP7-06` | SP7a | migration registry/runner, artifact digest, approval, rollout stop and recovery point |
| `D-SP7-07` | SP7a | canonical append-only audit, outbox and bounded retry; sink failure semantics |
| `D-SP7-08` | SP7a | health/metrics interfaces with bounded aggregates, redaction and explicit unknown |
| `D-SP7-09` | SP7a | external-effect reconcile ledger, generation fence and unknown-not-success rule |
| `D-SP7-10` | SP7a | raw-handle escape detector, repository-boundary ratchets and exceptions governance |
| `D-SP7-11` | SP7b | per-domain five-column migration contract: object/input/expected/evidence/gate, plus rollback |
| `D-SP7-12` | shared | retain/adapt/rewrite/new/retire map, conformance suite and delivery-verification handoff |

## 3. Hard invariants

- Package-external raw `sql.DB`, pool or transaction handles are zero. Escape through fields, aliases, interfaces, callbacks or return values is forbidden.
- Missing AccessCtx/DataScopeGrant, unknown enterprise mapping, unavailable policy, unavailable audit protection or unresolvable migration object fails closed.
- `WithAccess` selects the enterprise pool through opaque registry data, begins a transaction, applies access settings as the first statement, executes domain work, and commits outbox/audit atomically.
- Application predicates are primary; RLS is defense in depth. `USING` and `WITH CHECK` both exist. Runtime roles are non-superuser, `NOBYPASSRLS`, non-owner and have no DDL.
- Platform DB never stores enterprise business bodies. Enterprise DB never owns mutable platform root policy.
- Backup, replica and replication paths are explicitly outside RLS and require separate encryption, approval and audit controls.
- Migration success does not prove data semantics. Each SP7b owner supplies domain consistency queries and cutover gates.
- Board migration waits for its conformance suite; storage rewrite cannot discard existing concurrency semantics.

## 4. Acceptance

<!-- ACCEPTANCE-SP7-BEGIN -->

| ID | Scope | Criterion | Cannot prove |
|---|---|---|---|
| `AC-SP7-01` | architecture §17 PLT/PLT.Security rows | expected and implemented coverage sets equal; unknown set empty | semantic quality |
| `AC-SP7-02` | architecture interfaces `PLT-*` | every stable ID has schema, auth, idempotency/generation, transaction/effect, errors and audit | implementation correctness |
| `AC-SP7-03` | repository boundary | AST detector finds zero external raw-handle escapes and selftests cover alias/field/interface/callback/return variants | reflection or non-Go access |
| `AC-SP7-04` | access isolation | separate tests prove pre-query PEP rejection with query count zero, semantic denial, indistinguishable existence and minimized audit | production configuration |
| `AC-SP7-05` | RLS fixture | wrong enterprise, forged principal, missing context, owner role, BYPASSRLS, missing FORCE and missing WITH CHECK each fail | all PG versions |
| `AC-SP7-06` | migration | artifact/approval/generation verified; one enterprise failure stops later rollout; recovery point and domain consistency query required | irreversible recovery success |
| `AC-SP7-07` | SP7b handoff | every domain migration has object/input/expected/evidence/gate and rollback; SP7a does not invent schema | domain design quality |
| `AC-SP7-08` | review | alternating one-reviewer full-document rounds, all findings exposed, final PASS, no more than six rounds | reviewer expertise |

<!-- ACCEPTANCE-SP7-END -->

## 5. Review protocol

Each round selects exactly one of `opus50tczc2000` and `qwen38max`, alternating between rounds. The reviewer reads the entire contract/body/checker set and returns one complete findings list plus `PASS`, `NO`, or `INCONCLUSIVE`. The author addresses a whole round before the next full review. No single-detail ping-pong. At most six rounds.

<!-- REVIEW-PROTOCOL-BEGIN -->
| key | value |
|---|---|
| `max_rounds` | `6` |
| `reviewers_per_round` | `1` |
| `rotation` | `alternating` |
| `scope` | `full_document` |
| `exchange` | `whole_round` |
<!-- REVIEW-PROTOCOL-END -->
