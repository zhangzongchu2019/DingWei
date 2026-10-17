# B seat capacity — intermediate implementation, NOT final delivery

Recorded 2026-10-17T13:08:13Z. Baseline: integration/spec5ff-r22
`840741456534edb3b4f6457847aa670bba93d2d2`, tree
`3bcbff5ff5adbaad5956401d2a8e3fe6cfac73d9`.

This branch is an incomplete, reviewable checkpoint. It is not ready for
integration, release, deployment, or a final acceptance verdict.

## Implemented

- Pure `internal/seatcapacity` arithmetic: deduct explicit node reserves and
  non-managed occupation; charge managed reservations by the greater of actual
  resources and configured minima; compute CPU/memory/disk slot counts and their
  minimum. Unknown, invalid, future, or expired snapshots refuse admission.
- Distinguish a new allocation from restarting an existing reservation. Capacity
  loss refuses a new start without changing or stopping running seats.
- Node configuration for minimum seat CPU, memory, and disk, with bounded integer
  validation and policy-revision binding. Defaults: 1000 millicores, 1536 MiB,
  40 GiB. These values are not connected to admission yet.
- Five targeted tests, including net 8-core/16-GiB/320-GiB capacity admitting
  eight minimum reservations and refusing the ninth; deductions, independent
  resource exhaustion, unknown/stale snapshots, and configuration rejection.

## Verification actually run

`go test -count=1 -race -v ./internal/seatcapacity ./internal/nodeagent -run 'TestSeatCapacityMinimumConfiguration|TestOwnerEight|TestDimensions|TestUnknownIsNever|TestExistingStart'`

Result: rc0; five top-level tests passed; 13.826 seconds wall time. Raw log and
result JSON are adjacent under `raw/`. This is targeted evidence only. No full
nodeagent, canonical, browser, or withdrawal result is claimed for this checkpoint.

## Not implemented

Non-managed resource sampling and its unknown/age handling; atomic create/start
admission integration; capacity report protocol, Hub scheduling, and Console
dimensions/reasons; end-to-end gate negatives and withdrawals; complete canonical
(including disposable local PG) and nodeagent regression gates.

The calculator's snapshot is supplied by its caller; the tests do not establish
that a production sampler, CLI, Hub, or actual startup path enforces this rule.

## Current authority and interpretation

- Continue from r22 without rebasing. The leader adopted A1 and N2 into r23
  `fa07ddce`; B will be merged by the leader after review. This checkpoint does
  not independently import those increments.
- Pending an owner decision, 40 GiB is capacity reservation only. Existing 8/16
  GiB plan entitlements, user quotas, and running seats remain unchanged.
- The eight-seat arithmetic example uses **net allocatable** resources. Required
  node reserves and non-managed occupation are not set to zero to manufacture a
  passing example. Gross-versus-net example semantics and hard-node scope have
  been raised with the leader and remain to be resolved for integration.

## Schedule and boundaries

The 13:00Z ETA was missed; there is no tool/environment blocker. Remaining work
is primarily implementation, not just tests. Revised final-delivery ETA:
**2026-10-17T18:00:00Z**. This is the author's estimate, not reviewer acceptance.

Local source and tests only. No hosts, real Docker, external/production PG,
credentials, real Agents, jobs, builds for release, or deployment were touched.
Temporary local PG remains authorized for the later complete canonical gate.
