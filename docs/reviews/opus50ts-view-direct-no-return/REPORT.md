# S-VIEW-DIRECT-NO-RETURN — author delivery

Base: integration/spec5ff-r11, b0f449bd7b2883808c3bce322ede635d56aa6a83.
Branch: review/opus50ts/view-direct-no-return.
Developer: opus50ts; independent reviewer assigned: togpt60lunazzc.
Scope: local development and tests only; no integration merge, deployment or production access.

## Behavior and trust boundary

A human View direct input originates from the reserved workpulse address. Its
answer is already published to that session's View event stream. Treating that
address as an Agent reply destination produces a false groupAgentNotFound R1
failure, an error-dock event and (for a capable connected helper) delivery_failure.

New View v2 direct inputs explicitly carry reply_policy=no_reply. The automatic
reply path also recognizes historical direct inputs through the durable row's
view_inbound source type and Hub-attested, trusted human provenance, together
with the reserved workpulse sender bound to that same target key. This check
runs after the durable row's key/session has been validated against the
authenticated emitting session, and before Agent return routing.

Envelope source_kind, auth_mode, console_principal_id and client-provided
provenance are not authority. Authenticated Agent ingress still replaces the
attestation; a forged human claim retains the ordinary Agent return path.
There is no new owner/group exemption, no AMR parser change, no renderer/helper
change and no migration. Explicit mentions in an answer to a human View input
still route normally, including the resulting Agent reply to its actual origin.

## Regression evidence

The new suite uses signed View-service input, actual durable rows,
authenticated WebSocket session events and the normal AMR worker. It observes
the event ring used by View and the actual target transport, rather than
mocking replyToOrigin. It waits for the return outcome counter (closed alone
precedes reply routing).

- Same-owner and cross-owner direct input, each with new policy and a genuine
  Hub-attested historical missing-policy row: answer remains in View's stream;
  no R1 unavailable/routed outcome, error dock, delivery_failure or notice send.
- Forged human View metadata through Agent WS: persisted Agent provenance and
  ordinary cross-owner return to the true sending Agent.
- Negative durable-classification cases: wrong source type, absent provenance,
  Agent kind, untrusted decision, wrong attester, foreign key and Agent sender.
- A human View answer explicitly mentioning another Agent still routes and
  receives the Agent's response normally.
- Existing GroupReply AMR/MCP, membership revocation, true-source binding,
  channel one-hop, forged-return marker, cross-owner View reply isolation and
  mention-reply mirror regressions.

View evidence is the Hub event-stream boundary, not a browser or production
visual-acceptance claim. Historical shape is constructed by copying an actual
Hub-created View delivery and removing only its explicit reply policy.

## Withdrawals and non-PASS history

Two surgical product withdrawals each failed behavioral assertions (rc1):
removing the new explicit policy fails the new-input policy assertions;
disabling only the trusted historical View skip recreates R1
`自动回程投递失败:组内找不到该Agent` for both owners. Source was restored before
candidate regression runs.

The first local run could not create temporary test directories (ENOSPC), rc1;
it is retained as an environment non-PASS. Only completed task-private runtime
and unused private downloaded browser caches were reclaimed; shared Go caches,
evidence and installed Chrome were retained. The unchanged product tests then
ran successfully. An initial broad race run explicitly skipped GroupReplyMCP
without its opt-in interpreter; a separate enabled real-MCP race run passed.
All other environment/deferred skips are listed in RESULTS.json and are not
counted as passes.

Commands, results, durations, file hashes and log SHA256 values are recorded in
RESULTS.json. These are author results, not an independent review verdict.

Final results: full m8 rc0, 143.058s (642 top-level passes, four explicit
non-PASS skips); broad targeted race rc0, 72.044s; enabled real-MCP and direct
explicit-mention race rc0, 6.418s; canonical build rc0, 49.660s,
BUILD_TESTS_OK python=42 shell=2. Both surgical withdrawals rc1.
Diff check rc0. No product changes after these final runs.
