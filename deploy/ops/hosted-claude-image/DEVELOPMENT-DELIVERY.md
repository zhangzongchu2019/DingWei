# S-HOSTED-CLAUDE development delivery boundary

## Candidate

This candidate is based on `integration/spec5ff-r25` `031c4284`; the exact
delivery coordinate is recorded with the Git branch, not inferred from a local
worktree.  It defines the Claude 2.1.285 launcher, token-file ABI, static
unverified combo, minimum-plan admission coverage, locked offline input metadata
and a fixture-tested context packager.

## What passed locally

- `go test ./internal/hosted -run 'TestClaude(TokenFileABI|MinimumPlanRemainsUnverified)|TestStaticRegistriesFailClosed|TestMinimumPlanDefinitionAndNetCapacity' -count=1`
- `python3 -m unittest deploy/ops/hosted-claude-image/test_claude_start.py`
- Withdrawal: removing the Claude combo definition makes the Python fixture
  suite return non-zero; restoring its exact source returns it to green.

The static combo remains `verified:false`; therefore this development candidate
does not enable Claude seat creation or claim a verified provider integration.

## Explicit later execution gates

None of these are authorized or performed by this candidate:

1. A separately approved target-host window must obtain the three named
   artifacts, validate the locked NPM integrity/SHAs and Node SHA-256, and build
   only with 43.172.73.6's independent Engine.
2. The staged context must be checked against the approved H3 base context and
   exact source revision before the Engine is contacted.
3. An owner-approved, non-production CLI validation must prove the Claude MCP
   configuration and token-file provider ABI.  Only then may a separately
   reviewed change make the combo verified.
4. No real account, existing login state, credential import, Docker daemon,
   Hub, Agent, host or deployment is touched beforehand.
