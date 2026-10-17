# Minimum plan increment inside B — not final B delivery

Authority: `review/opus50tc2k/rulings` at
`4efad4a75fee05facca9337b0e9bd293a61a9361`,
`docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md`, items 8–9, and the subsequent
direct instruction to implement the new plan as B's independent commit.

Code commit: `1ed944134e11dae3c51e7727799e3882ca47983b`; tree:
`c8e42ba7395052dc5d4afb3def818f81ae330ccb`. Parent is the incomplete B checkpoint
`96b65de4392aed768548716ed28cab4101c83a27`, based on the required r22. This does
not rebase B or independently import r23's A1/N2.

## Behavior

- New `1c1.5g40`: one CPU, 1536 MiB memory, 40960 MiB disk. Existing `2c4g` and
  `4c8g` resources are unchanged. Unknown plans remain rejected.
- `PlanIDs` feeds Console options with the new plan first. Its name is
  `1C/1.5G/40G`, and JSON retains `mem_gb:1.5`. Console can select and submit its
  actual ID through the existing creation form.
- The normal creation path validates the plan and user quota, persists it, and
  sends 1536 MiB memory, 40960 MiB disk, 1024 CPU shares and (on a node advertising
  the existing hard-resource feature) a 1000-millicore hard limit. The node's
  actual Engine/cgroup behavior is not tested by these local fake-gateway tests.
- Usage aggregation counts the actual new plan, including disabled seats and
  excluding destroyed seats. Memory balance, error remaining values, and admin
  usage preserve half-GiB values; integer-valued existing responses keep the same
  JSON spelling and keys.
- Fresh schema accepts all three plans. An idempotent transactional replacement
  of the deployed plan CHECK admits the new ID without rewriting existing seats
  or user quotas. Deploying this code later invokes that schema upgrade through
  the normal wiring; no real database has been touched here.

The existing default user quota is unchanged: 64 GiB disk fits one new 40-GiB
plan, leaving 24 GiB. More such seats require sufficient existing or separately
authorized user quota. Node capacity and user quota remain separate gates.

Node management is the nodeagent system service, accounted in node reserve. This
increment creates no management Agent, seat, or identity.

## Local evidence

- Exact code commit: four minimum-plan tests under `-race` pass, no skips. They
  cover deployed CHECK upgrade twice, retained old rows/quotas, invalid-plan SQL
  rejection, per-dimension user-quota boundaries, disabled/destroyed usage,
  fractional options, and real orchestrator creation resource parameters.
- Actual HTTP routes: new plan options/create/debit plus the original admin
  quota-shape test pass under `-race`. One new seat leaves 6.5 GiB of the fixture's
  8-GiB memory quota; admin usage displays 1.5 GiB. Anonymous creation stays 401.
- Real Console page against loopback fixtures: 16 assertions pass, retaining
  the previous soft notice/consent/refusal checks and selecting/submitting the
  minimum plan. This is a browser fixture, not a real Hub or Agent pilot.
- Three isolated source withdrawals fail actual assertions: remove the plan
  entry, remove only the deployed CHECK migration, or restore integer memory
  truncation. Tests are unchanged. Restored exact code is green.

The plan's actual resources are also passed to B's pure calculator: **net**
8 CPUs / 16 GiB / 320 GiB admits eight and refuses the ninth for CPU and disk.
This is arithmetic coverage only. Actual admission integration in B is still
unfinished; no claim that a real create/start path already enforces that boundary.

An earlier hosted whole-package race run began before the final fractional-quota
display increment. It is not offered as the exact final-source full gate.
Complete canonical (including disposable local PG), nodeagent regression and
integrated capacity-path withdrawals remain for final B delivery.

## Scheduling and rollback limits

New B final-delivery estimate: **2026-10-17T20:00:00Z**, revised from 18:00Z for
this added scope. This report is a partial handoff, not final acceptance.

No running seat or entitlement was migrated. After new-plan rows exist, an old
binary without the new PlanByID mapping cannot correctly account for them; do
not assume a blind binary rollback is safe. A later operational rollback needs
an explicit compatibility/state plan and owner approval, without deleting user
rows or silently converting plans.

No hosts, real Docker daemon, external/production PG, real secrets/Agents,
offers/jobs or deployment were used. Only disposable local PG fixtures ran.
