# S-GROUP-CROSS-OWNER slice 1: author delivery

UTC: 2026-10-15T01:44Z. Base de7f879b3bef474f0b1c1d2621d1e6efbe78e1aa.
Product/test tip a5b6491c05093d81ec2a576cef7b4b2a0c529e0b; tree b84cfa69f523d647a62f08af6d5dac8279e12986.
This is author evidence, not independent CR or deployment authorization.

## Three-route inventory and implementation

1. View structured mention was blocked by the extra same-owner check at base view_mention_route.go:81–84 after MentionCandidates. Removed it. Cross-owner envelopes now use source_kind=agent, real source key/session, explicit sender-plus-View display label, cross_owner=true and no console principal/owner exemption. Same-owner shape remains unchanged. Group authorization is checked immediately before routing.
2. MCP/Agent direct exact full-name messages already passed RouteEnvelope's full-name fallback (base hub.go:3182+) and current AgentsCanCommunicate. Authenticated WS tests preserve this behavior and denial after membership changes.
3. AMR/public-full-name selectors were routed through the owner-only # selector (base hub.go:3659+). Exact public other-owner full names now reach the existing full-name group gate; short/fuzzy names stay owner-local. Explicit @member#session parsing is unchanged.

View reply display is explicitly included per follow-up authorization. Only a Hub-attested derived delivery can mirror the correlated target turn to its originating View. Current group membership is rechecked; removed/deleted groups stop publication. The mirror is display-only: no command or source-Agent injection. Its lookup cache is scoped by key/session/delivery, preventing another authenticated session from reusing a warmed delivery ID. WS admission discards client provenance, View attestation and source identity claims.

Other-owner candidates always insert FullName, even when their short name is unique. Same-owner unique short-name insertion remains unchanged. Actual candidates API and real browser input/send tests cover both.

Policy clarification: AdditionsClosed only stops new joins; it does not stop existing communication. LeaderOnly is preserved. Removing a member/deleting the group denies immediately. Max five View targets, no_ack, reliable offline queue/reconnect and mention_verbatim one-hop behavior are preserved.

## Validation and withdrawal

Commands from repository root (logs supplied):
- GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local TMPDIR=/tmp go test -race ./internal/identity ./internal/m8 -run '^(TestMentionCandidateOtherOwnerAlwaysFullName|TestNewPlatformMentionCandidates|TestGroupCrossOwner.*|TestViewMention.*|TestAgentFromRewrite.*)$' -count=1 -timeout 3m -v: rc0.
- NODE_PATH=/data/ai-dev-data/t114-wt/view-service/node_modules PW_CHANNEL=chrome node view-service/renderer-mention-candidates-regression.js: rc0.
- Same environment, renderer-mention-route-regression.js: rc0, 9 checks.
- PATH=<locked Python venv>/bin:$PATH PYTHONDONTWRITEBYTECODE=1 TMPDIR=/tmp bash scripts/run-build-tests.sh: rc0, python=42 shell=2.
- DW_SPEC_E2E_PYTHON=<locked Python> GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local TMPDIR=/tmp go test -json ./cmd/workpulse/... ./internal/m8/... ./internal/identity/... -count=1 -timeout 6m -v: final rc0; readable streamed -v output is FULL-GO-FINAL.log. FINAL-NONPASS.json lists every opt-in/deferred skip; none is counted as passed.

Seven independent Go-overlay withdrawals return rc1 at assertions: restore the View owner gate; restore owner-only exact selector; restore same-owner-only mirror; restore global delivery-ID cache; stop stripping View marker; stop stripping source identity claims; restore short insert for unique cross-owner candidate. Private overlays are not product commits; supplied logs show the failing assertions. All first six touch product bytes unchanged since 955bab37; candidate withdrawal runs on a5b6491c.

## Earlier non-PASS attempts retained

FULL-GO.log had two now-corrected test expectations: the old exact-fullname-cross-owner refusal expectation, and the new failure-receipt test searched for original text instead of the source request ID. Their corrected tests pass in final race/full logs. An early AMR test instance lacked its route callback; corrected test assembly, not product behavior. CANDIDATE-RACE initially expected Foreign.lbl; the fixture user's public short name is lb, so the expected full name was corrected to Foreign.lb. These are not counted as successful runs.

Inherited registration races seen across earlier full attempts:
- TestSessionAddressingFourFlows: sends before its home/target session is published.
- TestCrossMemberMentionRoutesToTargetSessionAndRepliesToSource: emits before target publication; r5 repeated baseline reproduces two failures in twenty.
- TestAgentNetworkSkillPushAckAndSessionSelectorEnvelope: #developer dispatch precedes developer publication, logs short_name_not_resolvable then times out. FULL-GO-THIRD rc1; baseline repeat happened to pass twenty, so no baseline failure is claimed for this case.
- TestTerminalViewCookiePersistsAndRestoresUnlock: st is nil at hub_test.go:4756, dereferenced under hub.mu; panic cleanup blocks on that same mutex. FULL-GO-SECOND was diagnostically interrupted with SIGQUIT, rc1, not a completed assertion run.

UNCHANGED-FIXTURE-PROOF.json proves hub_test.go, terminal.go, AMR/reply code, resolveOwnerSessionByShort and the complete session registration-before-receive block identical to r5. The earlier logs/stacks point to pre-publication access; the changed WS code only strips forged metadata later in the receive loop. Per leader ruling these are recorded separately as S-M8-FIXTURE-RACES, not silently fixed here. Final exact source full run is rc0; prior failures remain disclosed.

## Scope split

Agent-origin automatic A→B→A return and AMR default expect_reply are a separate second slice, review/opus50ts/group-cross-owner-reply (ETA 2026-10-15T03:00:00Z). This slice does not claim that work complete. MCP default reply policy remains caller-controlled. No renderer product change, migration, deployment or production access.
